package tunnel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTunnelSpecs(t *testing.T) {
	localTun := &Tunnel{
		ID:          "123",
		Name:        "Postgres",
		Type:        TypeLocal,
		Host:        "db-server",
		BindAddress: "127.0.0.1",
		LocalPort:   5432,
		RemoteHost:  "localhost",
		RemotePort:  5432,
	}

	if localTun.ForwardFlag() != "-L" {
		t.Errorf("Expected -L, got %s", localTun.ForwardFlag())
	}
	if localTun.ForwardSpec() != "127.0.0.1:5432:localhost:5432" {
		t.Errorf("Unexpected spec: %s", localTun.ForwardSpec())
	}

	remoteTun := &Tunnel{
		ID:          "456",
		Name:        "Web Hook",
		Type:        TypeRemote,
		Host:        "remote-vps",
		RemotePort:  8080,
		RemoteHost:  "127.0.0.1",
		LocalPort:   3000,
	}
	if remoteTun.ForwardFlag() != "-R" {
		t.Errorf("Expected -R, got %s", remoteTun.ForwardFlag())
	}
	if remoteTun.ForwardSpec() != "8080:127.0.0.1:3000" {
		t.Errorf("Unexpected remote spec: %s", remoteTun.ForwardSpec())
	}

	dynamicTun := &Tunnel{
		ID:          "789",
		Name:        "SOCKS Proxy",
		Type:        TypeDynamic,
		Host:        "proxy-host",
		BindAddress: "127.0.0.1",
		LocalPort:   1080,
	}
	if dynamicTun.ForwardFlag() != "-D" {
		t.Errorf("Expected -D, got %s", dynamicTun.ForwardFlag())
	}
	if dynamicTun.ForwardSpec() != "127.0.0.1:1080" {
		t.Errorf("Unexpected dynamic spec: %s", dynamicTun.ForwardSpec())
	}
}

func TestTunnelStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ssht-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "tunnels.json")
	store := &TunnelStore{filePath: filePath}

	tunnels, err := store.Load()
	if err != nil {
		t.Fatalf("Load on empty failed: %v", err)
	}
	if len(tunnels) != 0 {
		t.Fatalf("Expected 0 tunnels, got %d", len(tunnels))
	}

	tun := &Tunnel{
		ID:        "t1",
		Name:      "Test Tunnel",
		Type:      TypeLocal,
		Host:      "myserver",
		LocalPort: 8080,
	}

	if err := store.Save([]*Tunnel{tun}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load after save failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Test Tunnel" {
		t.Fatalf("Loaded unexpected data: %+v", loaded)
	}
}

func TestRunningTracker(t *testing.T) {
	tmpDir := t.TempDir()
	runPath := filepath.Join(tmpDir, "running.json")
	tracker := NewRunningTrackerWithPath(runPath)

	// Set running with current process PID (which is alive!)
	currentPid := os.Getpid()
	tracker.SetRunning("t1", currentPid, time.Now())

	records := tracker.Load()
	if rec, ok := records["t1"]; !ok || rec.PID != currentPid {
		t.Fatalf("Expected t1 to be recorded with PID %d, got %+v", currentPid, records)
	}

	// Set running with an invalid/dead PID (e.g. 99999999)
	tracker.SetRunning("t2", 99999999, time.Now())
	records = tracker.Load()
	if _, ok := records["t2"]; ok {
		t.Fatalf("Dead PID should have been cleaned up automatically by Load")
	}

	// Remove t1
	tracker.Remove("t1")
	records = tracker.Load()
	if len(records) != 0 {
		t.Fatalf("Expected 0 records after Remove, got %d", len(records))
	}
}

func TestGetLogNoDeadlock(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTunnelStoreWithPath(filepath.Join(tmpDir, "tunnels.json"))
	mgr, err := NewManagerWithStore(store)
	if err != nil {
		t.Fatalf("NewManagerWithStore failed: %v", err)
	}
	defer mgr.Close()

	tun := &Tunnel{
		ID:        "test1",
		Name:      "Test",
		LastError: "Some error occurred",
	}
	_ = mgr.AddTunnel(tun)

	// This must not deadlock (previously called FindTunnel inside RLock)
	log := mgr.GetLog("test1")
	if log != "Some error occurred" {
		t.Errorf("Expected 'Some error occurred', got %q", log)
	}
}
