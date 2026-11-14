package sshconfig

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// HostConfig represents an SSH host entry parsed from ~/.ssh/config.
type HostConfig struct {
	Aliases        []string          // e.g. ["194.31.223.176", "amnezia2germany"]
	HostName       string            // Destination host or IP
	User           string            // SSH username
	Port           int               // SSH port (defaults to 22)
	IdentityFile   string            // Path to private key
	Options        map[string]string // Extra options (e.g. ServerAliveInterval, etc.)
	RawLines       []string          // Original lines for faithful preservation
	FromKnownHosts bool              `json:"-"` // Listed from known_hosts, not a Host block
}

// DisplayName returns the primary identifier for display without redundant IPs.
func (h *HostConfig) DisplayName() string {
	for _, a := range h.Aliases {
		if !isIP(a) {
			return a
		}
	}
	if len(h.Aliases) > 0 {
		return h.Aliases[0]
	}
	if h.HostName != "" {
		return h.HostName
	}
	return "unknown"
}

// CustomName returns the user-given alias, or empty if only IP.
func (h *HostConfig) CustomName() string {
	for _, a := range h.Aliases {
		if !isIP(a) {
			return a
		}
	}
	if len(h.Aliases) > 0 {
		return h.Aliases[0]
	}
	return h.HostName
}

// PrimaryAlias returns the main alias to pass to `ssh <alias>`.
func (h *HostConfig) PrimaryAlias() string {
	for _, a := range h.Aliases {
		if !isIP(a) {
			return a
		}
	}
	if len(h.Aliases) > 0 {
		return h.Aliases[0]
	}
	return h.HostName
}

func isIP(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// HasProxyJump reports whether the host uses ProxyJump or ProxyCommand.
func (h *HostConfig) HasProxyJump() bool {
	if h.Options == nil {
		return false
	}
	for k, v := range h.Options {
		lk := strings.ToLower(k)
		if (lk == "proxyjump" || lk == "proxycommand") && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// GetConfigPath returns the absolute path to the OpenSSH user config.
// On Windows the profile can be exposed as USERPROFILE, HOME, or the
// token profile (long name vs 8.3). The directory that already has a
// config or known_hosts wins.
func GetConfigPath() (string, error) {
	homes := candidateHomes()
	if len(homes) == 0 {
		return "", fmt.Errorf("home directory is not defined")
	}

	var fallback string
	for _, home := range homes {
		cfg := filepath.Join(home, ".ssh", "config")
		if fallback == "" {
			fallback = cfg
		}
		if st, err := os.Stat(cfg); err == nil && !st.IsDir() {
			return cfg, nil
		}
	}
	for _, home := range homes {
		kh := filepath.Join(home, ".ssh", "known_hosts")
		if st, err := os.Stat(kh); err == nil && !st.IsDir() {
			return filepath.Join(home, ".ssh", "config"), nil
		}
	}
	return fallback, nil
}

func candidateHomes() []string {
	var homes []string
	seen := map[string]bool{}
	add := func(h string) {
		h = strings.TrimSpace(h)
		h = strings.TrimRight(h, `\/`)
		if h == "" {
			return
		}
		key := strings.ToLower(filepath.Clean(h))
		if seen[key] {
			return
		}
		seen[key] = true
		homes = append(homes, h)
	}

	if h, err := os.UserHomeDir(); err == nil {
		add(h)
	}
	if runtime.GOOS == "windows" {
		add(os.Getenv("USERPROFILE"))
		add(os.Getenv("HOME"))
		if d, p := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"); d != "" && p != "" {
			add(d + p)
		}
		if u, err := user.Current(); err == nil {
			add(u.HomeDir)
		}
	} else {
		add(os.Getenv("HOME"))
	}
	return homes
}

// LoadSSHConfig reads ~/.ssh/config (including Include) and adds servers
// that exist only in known_hosts.
func LoadSSHConfig() ([]*HostConfig, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	var hosts []*HostConfig
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		hosts, err = parseConfigFile(path, 0, map[string]struct{}{})
		if err != nil {
			return nil, err
		}
	}

	known, err := parseKnownHosts(filepath.Join(filepath.Dir(path), "known_hosts"))
	if err != nil {
		return hosts, err
	}
	return mergeKnownHosts(hosts, known), nil
}

// IsWildcardAlias reports whether an alias string contains wildcard glob characters (*, ?).
func IsWildcardAlias(alias string) bool {
	return strings.ContainsAny(alias, "*?")
}

// IsWildcardHost reports whether all aliases of the host are wildcard patterns.
func (h *HostConfig) IsWildcardHost() bool {
	if len(h.Aliases) == 0 {
		return true
	}
	for _, a := range h.Aliases {
		if !IsWildcardAlias(a) {
			return false
		}
	}
	return true
}

func parseConfigReader(r io.Reader) ([]*HostConfig, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parseConfigText(decodeSSHText(b), "", 0, map[string]struct{}{})
}

func parseConfigFile(path string, depth int, seen map[string]struct{}) ([]*HostConfig, error) {
	if depth > 8 {
		return nil, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	key := strings.ToLower(abs)
	if _, ok := seen[key]; ok {
		return nil, nil
	}
	seen[key] = struct{}{}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseConfigText(decodeSSHText(b), filepath.Dir(path), depth, seen)
}

func parseConfigText(text, baseDir string, depth int, seen map[string]struct{}) ([]*HostConfig, error) {
	var hosts []*HostConfig
	var current *HostConfig

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if current != nil {
				current.RawLines = append(current.RawLines, line)
			}
			continue
		}

		key, fields := sshKeyAndFields(trimmed)
		if key == "" {
			continue
		}

		if key == "match" {
			if current != nil && !current.IsWildcardHost() {
				hosts = append(hosts, current)
			}
			current = nil
			continue
		}

		if key == "include" && baseDir != "" {
			if current != nil && !current.IsWildcardHost() {
				hosts = append(hosts, current)
			}
			current = nil
			included, err := expandInclude(strings.Join(fields[1:], " "), baseDir, depth, seen)
			if err != nil {
				return nil, err
			}
			hosts = append(hosts, included...)
			continue
		}

		if key == "host" {
			if current != nil && !current.IsWildcardHost() {
				hosts = append(hosts, current)
			}
			aliases := make([]string, 0, len(fields)-1)
			for _, a := range fields[1:] {
				a = unquoteSSH(a)
				if a != "" {
					aliases = append(aliases, a)
				}
			}
			current = &HostConfig{
				Aliases:  aliases,
				Port:     22,
				Options:  make(map[string]string),
				RawLines: []string{line},
			}
			continue
		}

		if current == nil {
			continue
		}

		current.RawLines = append(current.RawLines, line)
		val := ""
		if len(fields) > 1 {
			val = unquoteSSH(strings.Join(fields[1:], " "))
		}

		switch key {
		case "hostname":
			current.HostName = val
		case "user":
			current.User = val
		case "port":
			if p, err := strconv.Atoi(val); err == nil && p > 0 {
				current.Port = p
			}
		case "identityfile":
			current.IdentityFile = val
		default:
			current.Options[fields[0]] = val
		}
	}

	if current != nil && !current.IsWildcardHost() {
		hosts = append(hosts, current)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hosts, nil
}

func sshKeyAndFields(line string) (string, []string) {
	s := strings.TrimSpace(line)
	if s == "" {
		return "", nil
	}
	i := strings.IndexAny(s, " \t=")
	var key, rest string
	if i < 0 {
		key = s
	} else {
		key = s[:i]
		rest = strings.TrimLeft(s[i:], " \t=")
	}
	fields := []string{key}
	if rest != "" {
		fields = append(fields, strings.Fields(rest)...)
	}
	return strings.ToLower(key), fields
}

func unquoteSSH(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func expandInclude(pattern, baseDir string, depth int, seen map[string]struct{}) ([]*HostConfig, error) {
	var hosts []*HostConfig
	for _, part := range strings.Fields(pattern) {
		part = expandTilde(part)
		if !filepath.IsAbs(part) {
			part = filepath.Join(baseDir, part)
		}
		matches, err := filepath.Glob(part)
		if err != nil || len(matches) == 0 {
			continue
		}
		sort.Strings(matches)
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || st.IsDir() {
				continue
			}
			included, err := parseConfigFile(m, depth+1, seen)
			if err != nil {
				continue
			}
			hosts = append(hosts, included...)
		}
	}
	return hosts, nil
}

func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

func decodeSSHText(b []byte) string {
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		b = b[3:]
	}
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) {
		return decodeUTF16(b[2:], true)
	}
	if bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		return decodeUTF16(b[2:], false)
	}
	return string(b)
}

func decodeUTF16(b []byte, little bool) string {
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if little {
			u[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
		} else {
			u[i] = uint16(b[i*2])<<8 | uint16(b[i*2+1])
		}
	}
	return string(utf16.Decode(u))
}

func parseKnownHosts(path string) ([]*HostConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var hosts []*HostConfig
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, port := splitKnownHost(fields[0])
		if name == "" || strings.ContainsAny(name, "*?") {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		hosts = append(hosts, &HostConfig{
			Aliases:        []string{name},
			HostName:       name,
			Port:           port,
			Options:        map[string]string{},
			FromKnownHosts: true,
		})
	}
	return hosts, scanner.Err()
}

func splitKnownHost(token string) (string, int) {
	if i := strings.IndexByte(token, ','); i >= 0 {
		token = token[:i]
	}
	port := 22
	if strings.HasPrefix(token, "[") {
		end := strings.IndexByte(token, ']')
		if end > 1 {
			name := token[1:end]
			if strings.HasPrefix(token[end:], "]:") {
				if p, err := strconv.Atoi(token[end+2:]); err == nil && p > 0 {
					port = p
				}
			}
			return name, port
		}
	}
	return token, port
}

func mergeKnownHosts(config, known []*HostConfig) []*HostConfig {
	have := map[string]bool{}
	for _, h := range config {
		for _, a := range h.Aliases {
			have[strings.ToLower(a)] = true
		}
		if h.HostName != "" {
			have[strings.ToLower(h.HostName)] = true
		}
	}
	for _, h := range known {
		if have[strings.ToLower(h.HostName)] {
			continue
		}
		config = append(config, h)
	}
	return config
}

// DangerousDirectives contains directives that could lead to arbitrary execution or unexpected inclusion.
var DangerousDirectives = map[string]bool{
	"proxycommand":       true,
	"localcommand":       true,
	"remotecommand":      true,
	"permitlocalcommand": true,
	"include":            true,
	"match":              true,
}

// IsDangerousOption checks whether an SSH directive key is risky.
func IsDangerousOption(key string) bool {
	return DangerousDirectives[strings.ToLower(strings.TrimSpace(key))]
}

// sanitizeSSHValue ensures a field contains no CRLF / newline characters.
func sanitizeSSHValue(field, val string) error {
	if strings.ContainsAny(val, "\r\n") {
		return fmt.Errorf("field %s contains illegal newline characters: %q", field, val)
	}
	return nil
}

// ValidateHostConfig validates host structure and checks against CRLF injection.
func ValidateHostConfig(host *HostConfig) error {
	if host == nil {
		return fmt.Errorf("host config is nil")
	}
	if len(host.Aliases) == 0 {
		return fmt.Errorf("host aliases cannot be empty")
	}
	for _, a := range host.Aliases {
		if err := sanitizeSSHValue("alias", a); err != nil {
			return err
		}
	}
	if err := sanitizeSSHValue("HostName", host.HostName); err != nil {
		return err
	}
	if err := sanitizeSSHValue("User", host.User); err != nil {
		return err
	}
	if err := sanitizeSSHValue("IdentityFile", host.IdentityFile); err != nil {
		return err
	}
	for k, v := range host.Options {
		if err := sanitizeSSHValue("option key", k); err != nil {
			return err
		}
		if err := sanitizeSSHValue("option value", v); err != nil {
			return err
		}
	}
	return nil
}

// WriteAtomic writes data to a temporary file and atomically renames it.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	if err := os.Rename(tmpFile, path); err != nil {
		_ = os.Remove(path)
		if err := os.Rename(tmpFile, path); err != nil {
			_ = os.Remove(tmpFile)
			return err
		}
	}

	return nil
}

// backupConfigFile safely saves a backup of existing config without destroying previous backups.
func backupConfigFile(path string, content []byte) {
	if len(content) == 0 {
		return
	}
	// Always keep a primary .bak if not already there
	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		_ = os.WriteFile(bakPath, content, 0600)
	}
	// Also keep a timestamped backup
	timeBak := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	_ = os.WriteFile(timeBak, content, 0600)
}

// AppendHost appends a new host entry to ~/.ssh/config.
func AppendHost(host *HostConfig) error {
	if host.IsWildcardHost() {
		return fmt.Errorf("wildcard hosts (e.g. Host *) cannot be added as individual servers")
	}

	if err := ValidateHostConfig(host); err != nil {
		return err
	}

	// Reject dangerous options
	for k := range host.Options {
		if IsDangerousOption(k) {
			return fmt.Errorf("directive %q is not permitted for security reasons", k)
		}
	}

	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	var existing []byte
	if content, err := os.ReadFile(path); err == nil {
		existing = content
	}

	backupConfigFile(path, existing)

	var sb strings.Builder
	if len(existing) > 0 {
		sb.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Host %s\n", strings.Join(host.Aliases, " ")))
	if host.HostName != "" {
		sb.WriteString(fmt.Sprintf("  HostName %s\n", host.HostName))
	}
	if host.User != "" {
		sb.WriteString(fmt.Sprintf("  User %s\n", host.User))
	}
	if host.Port > 0 && host.Port != 22 {
		sb.WriteString(fmt.Sprintf("  Port %d\n", host.Port))
	}
	if host.IdentityFile != "" {
		sb.WriteString(fmt.Sprintf("  IdentityFile %s\n", host.IdentityFile))
	}
	for k, v := range host.Options {
		sb.WriteString(fmt.Sprintf("  %s %s\n", k, v))
	}

	return WriteAtomic(path, []byte(sb.String()), 0600)
}

// UpdateHostAlias renames/adds an alias to an existing host in ~/.ssh/config.
func UpdateHostAlias(target *HostConfig, newAlias string) error {
	newAlias = strings.TrimSpace(newAlias)
	if newAlias == "" {
		return fmt.Errorf("alias cannot be empty")
	}
	if err := sanitizeSSHValue("alias", newAlias); err != nil {
		return err
	}

	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	matched := false

	targetMatch := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || strings.ToLower(fields[0]) != "host" {
			return false
		}
		currAliases := fields[1:]
		if len(currAliases) != len(target.Aliases) {
			return false
		}
		for i, a := range currAliases {
			if a != target.Aliases[i] {
				return false
			}
		}
		return true
	}

	for _, line := range lines {
		if !matched && targetMatch(line) {
			matched = true
			var newAliases []string
			newAliases = append(newAliases, newAlias)
			for _, a := range target.Aliases {
				if a != newAlias && !isIP(a) {
					newAliases = append(newAliases, a)
				}
			}

			// Preserve leading indentation if any
			indent := ""
			for _, r := range line {
				if r == ' ' || r == '\t' {
					indent += string(r)
				} else {
					break
				}
			}

			newLines = append(newLines, fmt.Sprintf("%sHost %s", indent, strings.Join(newAliases, " ")))

			// If target host had no explicit HostName directive, its original alias (e.g. IP) served as destination.
			// When renaming, inject explicit "HostName <old-target>" so the connection IP/address is never lost!
			if target.HostName == "" && len(target.Aliases) > 0 {
				oldTarget := target.Aliases[0]
				if oldTarget != newAlias {
					newLines = append(newLines, fmt.Sprintf("%s  HostName %s", indent, oldTarget))
					target.HostName = oldTarget
				}
			}

			target.Aliases = newAliases
		} else {
			newLines = append(newLines, line)
		}
	}

	if !matched {
		return fmt.Errorf("host not found in config")
	}

	backupConfigFile(path, content)

	output := strings.Join(newLines, "\n")
	return WriteAtomic(path, []byte(output), 0600)
}

// DeleteHost removes a host entry and its directives from ~/.ssh/config.
func DeleteHost(target *HostConfig) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	inTargetBlock := false
	deleted := false

	targetMatch := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || strings.ToLower(fields[0]) != "host" {
			return false
		}
		currAliases := fields[1:]
		if len(currAliases) != len(target.Aliases) {
			return false
		}
		for i, a := range currAliases {
			if a != target.Aliases[i] {
				return false
			}
		}
		return true
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)

		// Check if a new section starts (Host OR Match)
		if len(fields) >= 1 {
			secKey := strings.ToLower(fields[0])
			if secKey == "host" || secKey == "match" {
				if inTargetBlock {
					inTargetBlock = false
				}
				if secKey == "host" && !deleted && targetMatch(line) {
					inTargetBlock = true
					deleted = true
					continue
				}
			}
		}

		if inTargetBlock {
			continue
		}

		newLines = append(newLines, line)
	}

	if !deleted {
		return fmt.Errorf("host not found in config")
	}

	backupConfigFile(path, content)

	output := strings.Join(newLines, "\n")
	return WriteAtomic(path, []byte(output), 0600)
}
