package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tunnel"
)

// BackupPackage contains all persistent configuration and metadata.
type BackupPackage struct {
	Version      string                         `json:"version"`
	CreatedAt    time.Time                      `json:"created_at"`
	Description  string                         `json:"description,omitempty"`
	Tunnels      []*tunnel.Tunnel               `json:"tunnels"`
	HostsMetadata map[string]*sshconfig.HostMeta `json:"hosts_metadata"`
	HostsConfig  []*sshconfig.HostConfig        `json:"hosts_config,omitempty"`
}

// DefaultBackupPath returns a timestamped backup path in user's home directory.
func DefaultBackupPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dateStr := time.Now().Format("2006-01-02_15-04-05")
	return filepath.Join(home, fmt.Sprintf("ssht_backup_%s.json", dateStr))
}

// Export creates a full backup JSON file.
func Export(targetPath string, tm *tunnel.Manager, ms *sshconfig.MetadataStore) (string, error) {
	if targetPath == "" {
		targetPath = DefaultBackupPath()
	}

	hosts, err := sshconfig.LoadSSHConfig()
	if err != nil {
		hosts = []*sshconfig.HostConfig{}
	}

	tunnels := tm.GetTunnels()
	if tunnels == nil {
		tunnels = []*tunnel.Tunnel{}
	}

	ms.DataLock()
	metaCopy := make(map[string]*sshconfig.HostMeta)
	for k, v := range ms.Data {
		metaCopy[k] = &sshconfig.HostMeta{
			LastConnected: v.LastConnected,
		}
	}
	ms.DataUnlock()

	pkg := BackupPackage{
		Version:       "1.0.0",
		CreatedAt:     time.Now(),
		Description:   "SSHT Complete Backup",
		Tunnels:       tunnels,
		HostsMetadata: metaCopy,
		HostsConfig:   hosts,
	}

	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("ошибка сериализации: %w", err)
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("ошибка создания каталога: %w", err)
	}

	if err := os.WriteFile(targetPath, data, 0600); err != nil {
		return "", fmt.Errorf("ошибка записи файла: %w", err)
	}

	return targetPath, nil
}

// ImportResult reports statistics of imported items.
type ImportResult struct {
	TunnelsImported int
	HostsAdded      int
	MetaImported    int
}

// Import restores configuration from a backup file.
func Import(sourcePath string, tm *tunnel.Manager, ms *sshconfig.MetadataStore, overwrite bool) (*ImportResult, error) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл: %w", err)
	}

	var pkg BackupPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("неверный формат резервной копии: %w", err)
	}

	result := &ImportResult{}

	// 1. Restore Tunnels
	if overwrite {
		var newTunnels []*tunnel.Tunnel
		seen := make(map[string]bool)
		for _, t := range pkg.Tunnels {
			if strings.ContainsAny(t.Host, "\r\n") || strings.ContainsAny(t.Name, "\r\n") ||
				strings.ContainsAny(t.RemoteHost, "\r\n") || strings.ContainsAny(t.BindAddress, "\r\n") {
				continue
			}
			if t.ID != "" && seen[t.ID] {
				continue
			}
			if t.ID != "" {
				seen[t.ID] = true
			}
			t.Status = tunnel.StatusInactive
			t.PID = 0
			newTunnels = append(newTunnels, t)
		}
		_ = tm.SetTunnels(newTunnels)
		result.TunnelsImported = len(newTunnels)
	} else {
		existingTunnels := tm.GetTunnels()
		tunnelMap := make(map[string]bool)
		for _, t := range existingTunnels {
			tunnelMap[t.ID] = true
		}
		for _, t := range pkg.Tunnels {
			if strings.ContainsAny(t.Host, "\r\n") || strings.ContainsAny(t.Name, "\r\n") ||
				strings.ContainsAny(t.RemoteHost, "\r\n") || strings.ContainsAny(t.BindAddress, "\r\n") {
				continue
			}
			if !tunnelMap[t.ID] {
				t.Status = tunnel.StatusInactive
				t.PID = 0
				_ = tm.AddTunnel(t)
				tunnelMap[t.ID] = true
				result.TunnelsImported++
			}
		}
	}

	// 2. Restore Hosts Metadata
	ms.DataLock()
	for k, v := range pkg.HostsMetadata {
		if strings.ContainsAny(k, "\r\n") {
			continue
		}
		if overwrite {
			ms.Data[k] = v
			result.MetaImported++
		} else {
			if _, exists := ms.Data[k]; !exists {
				ms.Data[k] = v
				result.MetaImported++
			}
		}
	}
	ms.DataUnlock()
	_ = ms.Save()

	// 3. Restore Hosts to ~/.ssh/config (append any missing hosts)
	currentHosts, _ := sshconfig.LoadSSHConfig()
	knownAliases := make(map[string]bool)
	for _, ch := range currentHosts {
		for _, a := range ch.Aliases {
			knownAliases[a] = true
		}
	}

	for _, bh := range pkg.HostsConfig {
		// Reject wildcard patterns (e.g. Host *) from being imported as server hosts
		if bh.IsWildcardHost() {
			continue
		}
		// Reject CRLF injection in any field
		if err := sshconfig.ValidateHostConfig(bh); err != nil {
			continue
		}
		// Strip dangerous directives to prevent arbitrary code execution on import
		for optKey := range bh.Options {
			if sshconfig.IsDangerousOption(optKey) {
				delete(bh.Options, optKey)
			}
		}

		isNew := false
		for _, a := range bh.Aliases {
			if !knownAliases[a] {
				isNew = true
				break
			}
		}
		if isNew {
			if err := sshconfig.AppendHost(bh); err == nil {
				result.HostsAdded++
				for _, a := range bh.Aliases {
					knownAliases[a] = true
				}
			}
		}
	}

	return result, nil
}
