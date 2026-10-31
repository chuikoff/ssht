package backup

import (
	"os"
	"path/filepath"
	"testing"

	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tunnel"
)

func TestExportAndImport(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ssht-backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backupFile := filepath.Join(tmpDir, "backup.json")
	tunnels1File := filepath.Join(tmpDir, "tunnels1.json")
	tunnels2File := filepath.Join(tmpDir, "tunnels2.json")
	meta1File := filepath.Join(tmpDir, "meta1.json")
	meta2File := filepath.Join(tmpDir, "meta2.json")

	store1 := tunnel.NewTunnelStoreWithPath(tunnels1File)
	manager, err := tunnel.NewManagerWithStore(store1)
	if err != nil {
		t.Fatalf("Failed to create tunnel manager: %v", err)
	}

	tun := &tunnel.Tunnel{
		ID:        "b-tun-1",
		Name:      "Backup Test Tunnel",
		Type:      tunnel.TypeLocal,
		Host:      "test-host",
		LocalPort: 9090,
	}
	_ = manager.AddTunnel(tun)

	metaStore := sshconfig.NewMetadataStoreWithPath(meta1File)
	metaStore.RecordConnection("test-host")

	// Export
	exportedPath, err := Export(backupFile, manager, metaStore)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if exportedPath != backupFile {
		t.Errorf("Expected path %s, got %s", backupFile, exportedPath)
	}

	// Verify file exists and is non-empty
	info, err := os.Stat(backupFile)
	if err != nil || info.Size() == 0 {
		t.Fatalf("Backup file was not created properly: %v", err)
	}

	// Import into new clean manager
	store2 := tunnel.NewTunnelStoreWithPath(tunnels2File)
	newManager, _ := tunnel.NewManagerWithStore(store2)
	newMetaStore := sshconfig.NewMetadataStoreWithPath(meta2File)

	// Import with overwrite=true
	res, err := Import(backupFile, newManager, newMetaStore, true)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if res.TunnelsImported != 1 {
		t.Errorf("Expected 1 tunnel imported, got %d", res.TunnelsImported)
	}

	// Verify tunnel restored
	found := false
	for _, it := range newManager.GetTunnels() {
		if it.ID == "b-tun-1" && it.Name == "Backup Test Tunnel" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Exported tunnel was not found after import")
	}
}
