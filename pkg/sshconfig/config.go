package sshconfig

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// DisplayName returns the primary identifier for display.
func (h *HostConfig) DisplayName() string {
	if len(h.Aliases) > 1 {
		var custom string
		var ip string
		for _, a := range h.Aliases {
			if !isIP(a) && custom == "" {
				custom = a
			} else if isIP(a) && ip == "" {
				ip = a
			}
		}
		if custom != "" && ip != "" {
			return fmt.Sprintf("%s (%s)", custom, ip)
		}
		if custom != "" {
			return custom
		}
		return strings.Join(h.Aliases, ", ")
	}
	if len(h.Aliases) == 1 {
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

		if key == "host" {
			if current != nil {
				hosts = append(hosts, current)
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
			// Lines before first Host block
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

	if current != nil {
		hosts = append(hosts, current)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return hosts, nil
}

// AppendHost appends a new host entry to ~/.ssh/config.
func AppendHost(host *HostConfig) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Ensure ~/.ssh directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	var sb strings.Builder
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

	_, err = f.WriteString(sb.String())
	return err
}

// UpdateHostAlias renames/adds an alias to an existing host in ~/.ssh/config.
func UpdateHostAlias(target *HostConfig, newAlias string) error {
	newAlias = strings.TrimSpace(newAlias)
	if newAlias == "" {
		return fmt.Errorf("alias cannot be empty")
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

	// Target matching: matches if any alias matches or first line matches
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
			// Build new aliases list with newAlias at front
			var newAliases []string
			newAliases = append(newAliases, newAlias)
			for _, a := range target.Aliases {
				if a != newAlias {
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
			target.Aliases = newAliases
		} else {
			newLines = append(newLines, line)
		}
	}

	if !matched {
		return fmt.Errorf("host not found in config")
	}

	// Backup existing config
	_ = os.WriteFile(path+".bak", content, 0600)

	// Write updated file
	output := strings.Join(newLines, "\n")
	return os.WriteFile(path, []byte(output), 0600)
}

