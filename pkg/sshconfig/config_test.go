package sshconfig

import (
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
	if h3.DisplayName() != "amnezia2germany (194.31.223.176)" {
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
