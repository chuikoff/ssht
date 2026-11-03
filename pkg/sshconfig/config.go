package sshconfig

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HostConfig represents an SSH host entry parsed from ~/.ssh/config.
type HostConfig struct {
	Aliases      []string          // e.g. ["194.31.223.176", "amnezia2germany"]
	HostName     string            // Destination host or IP
	User         string            // SSH username
	Port         int               // SSH port (defaults to 22)
	IdentityFile string            // Path to private key
	Options      map[string]string // Extra options (e.g. ServerAliveInterval, etc.)
	RawLines     []string          // Original lines for faithful preservation
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

// GetConfigPath returns the absolute path to ~/.ssh/config.
func GetConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// LoadSSHConfig reads and parses ~/.ssh/config.
func LoadSSHConfig() ([]*HostConfig, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []*HostConfig{}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseConfigReader(file)
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
	var hosts []*HostConfig
	var current *HostConfig

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if current != nil {
				current.RawLines = append(current.RawLines, line)
			}
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}

		key := strings.ToLower(fields[0])

		if key == "match" {
			if current != nil {
				if !current.IsWildcardHost() {
					hosts = append(hosts, current)
				}
				current = nil
			}
			continue
		}

		if key == "host" {
			if current != nil {
				if !current.IsWildcardHost() {
					hosts = append(hosts, current)
				}
			}
			current = &HostConfig{
				Aliases:  fields[1:],
				Port:     22,
				Options:  make(map[string]string),
				RawLines: []string{line},
			}
			continue
		}

		if current == nil {
			// Directives before first Host block or inside Match block
			continue
		}

		current.RawLines = append(current.RawLines, line)

		val := ""
		if len(fields) > 1 {
			val = strings.Join(fields[1:], " ")
			// Support key = val or key val
			if fields[1] == "=" && len(fields) > 2 {
				val = strings.Join(fields[2:], " ")
			}
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

