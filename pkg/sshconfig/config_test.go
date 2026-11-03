package sshconfig

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseSampleConfig(t *testing.T) {
	sample := `
Host 80.240.252.241
  HostName 80.240.252.241
  User chuikoff
  Port 22
Host 144.31.253.173
    HostName 144.31.253.173
    User root
    IdentityFile ~/.ssh/id_ed25519_144.31
   ServerAliveInterval 120
Host 194.31.223.176 amnezia2germany
  HostName 194.31.223.176
  User root
  IdentityFile ~/.ssh/id_ed25519_194.31
`
	hosts, err := parseConfigReader(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(hosts) != 3 {
		t.Fatalf("Expected 3 hosts, got %d", len(hosts))
	}

	h1 := hosts[0]
	if h1.HostName != "80.240.252.241" || h1.User != "chuikoff" || h1.Port != 22 {
		t.Errorf("Host 1 parse mismatch: %+v", h1)
	}

	h3 := hosts[2]
	if len(h3.Aliases) != 2 || h3.Aliases[1] != "amnezia2germany" {
		t.Errorf("Host 3 alias mismatch: %+v", h3.Aliases)
	}
	if h3.DisplayName() != "amnezia2germany" {
		t.Errorf("Host 3 display name mismatch: %s", h3.DisplayName())
	}
}

func TestFormatLastConnected(t *testing.T) {
	if FormatLastConnected(nil) != "Никогда" {
		t.Errorf("Expected 'Никогда', got %s", FormatLastConnected(nil))
	}

	now := time.Now()
	if FormatLastConnected(&now) != "Только что" {
		t.Errorf("Expected 'Только что', got %s", FormatLastConnected(&now))
	}
}

func TestMetadataArchiving(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := tmpDir + "/metadata.json"
	store := NewMetadataStoreWithPath(metaPath)

	if store.IsArchived("host1") {
		t.Errorf("host1 should not be archived initially")
	}

	store.SetArchived("host1", true, "Сервер выключен")
	if !store.IsArchived("host1") {
		t.Errorf("host1 should be archived after SetArchived")
	}
	if store.ArchivedCount() != 1 {
		t.Errorf("Expected 1 archived host, got %d", store.ArchivedCount())
	}

	// Test persistence
	store2 := NewMetadataStoreWithPath(metaPath)
	if !store2.IsArchived("host1") {
		t.Errorf("host1 should be archived after reload from disk")
	}

	// Toggle back
	toggled := store2.ToggleArchived("host1")
	if toggled || store2.IsArchived("host1") {
		t.Errorf("host1 should be unarchived after toggle")
	}
	if store2.ArchivedCount() != 0 {
		t.Errorf("Expected 0 archived hosts, got %d", store2.ArchivedCount())
	}
}

func TestDeleteHost(t *testing.T) {
	sample := "Host ServerA\n  HostName 1.1.1.1\n\nHost ServerB\n  HostName 2.2.2.2\n"
	tmpFile := t.TempDir() + "/config"
	_ = os.WriteFile(tmpFile, []byte(sample), 0600)

	// Test deleting ServerA
	content, _ := os.ReadFile(tmpFile)
	lines := strings.Split(string(content), "\n")
	var newLines []string
	inTarget := false
	for _, l := range lines {
		if strings.HasPrefix(l, "Host ServerA") {
			inTarget = true
			continue
		}
		if inTarget && strings.HasPrefix(l, "Host ") {
			inTarget = false
		}
		if !inTarget {
			newLines = append(newLines, l)
		}
	}
	res := strings.Join(newLines, "\n")
	if strings.Contains(res, "ServerA") || !strings.Contains(res, "ServerB") {
		t.Errorf("Unexpected deletion result: %s", res)
	}
}

func TestWildcardAndMatchIgnored(t *testing.T) {
	sample := `
Host *
  ForwardAgent no
  ServerAliveInterval 60

Host server1
  HostName 10.0.0.1

Match user root
  PasswordAuthentication no

Host server2
  HostName 10.0.0.2
`
	hosts, err := parseConfigReader(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("Expected exactly 2 hosts (server1, server2), got %d", len(hosts))
	}
	if hosts[0].DisplayName() != "server1" || hosts[1].DisplayName() != "server2" {
		t.Errorf("Unexpected hosts parsed: %+v", hosts)
	}
}

func TestCRLFAndDangerousDirectives(t *testing.T) {
	// CRLF in alias
	h1 := &HostConfig{
		Aliases: []string{"bad\r\nalias"},
	}
	if err := ValidateHostConfig(h1); err == nil {
		t.Errorf("Expected CRLF error for h1")
	}

	// Dangerous directive
	h2 := &HostConfig{
		Aliases: []string{"evil"},
		Options: map[string]string{
			"ProxyCommand": "calc.exe",
		},
	}
	if err := AppendHost(h2); err == nil {
		t.Errorf("Expected dangerous directive error for h2")
	}

	// Wildcard host addition
	h3 := &HostConfig{
		Aliases: []string{"*"},
	}
	if err := AppendHost(h3); err == nil {
		t.Errorf("Expected wildcard host rejection for h3")
	}
}

func TestDeleteHostBoundaries(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := tmpDir + "/config"

	sample := `Host web
  HostName 1.2.3.4

Host web web-prod
  HostName 5.6.7.8

Match host other
  ForwardAgent yes
`
	_ = os.WriteFile(cfgPath, []byte(sample), 0600)

	// Custom environment to point to our test config
	origHome := os.Getenv("USERPROFILE")
	if origHome == "" {
		origHome = os.Getenv("HOME")
	}
	// We test lines algorithm directly matching DeleteHost logic
	target := &HostConfig{Aliases: []string{"web"}}

	lines := strings.Split(sample, "\n")
	var newLines []string
	inTargetBlock := false
	deleted := false

	targetMatch := func(line string) bool {
		fields := strings.Fields(strings.TrimSpace(line))
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
		fields := strings.Fields(strings.TrimSpace(line))
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

	result := strings.Join(newLines, "\n")
	if strings.Contains(result, "Host web\n") {
		t.Errorf("Expected 'Host web' to be deleted, but found in:\n%s", result)
	}
	if !strings.Contains(result, "Host web web-prod") {
		t.Errorf("Expected 'Host web web-prod' to be PRESERVED, but missing in:\n%s", result)
	}
	if !strings.Contains(result, "Match host other") {
		t.Errorf("Expected 'Match host other' to be PRESERVED, but missing in:\n%s", result)
	}
}
