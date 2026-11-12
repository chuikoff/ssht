package tui

import (
	"path/filepath"
	"testing"

	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tunnel"
)

func TestInitChecksActiveHostsOnly(t *testing.T) {
	dir := t.TempDir()
	meta := sshconfig.NewMetadataStoreWithPath(filepath.Join(dir, "meta.json"))
	store := tunnel.NewTunnelStoreWithPath(filepath.Join(dir, "tunnels.json"))
	mgr, err := tunnel.NewManagerWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Close() })

	app := NewAppModel(mgr, meta)
	app.HostsView.Hosts = []*sshconfig.HostConfig{
		{HostName: "10.0.0.1", Aliases: []string{"alpha"}},
		{HostName: "10.0.0.2", Aliases: []string{"beta"}},
	}
	meta.ToggleArchived("beta")
	app.HostsView.ApplyFilter()

	// Do not run the returned command: that would dial the hosts.
	_ = app.Init()

	if app.Notification != "Проверка доступности серверов..." {
		t.Fatalf("notification %q", app.Notification)
	}
	if meta.Get("alpha").Status != sshconfig.StatusChecking {
		t.Fatalf("alpha status %s, want CHECKING", meta.Get("alpha").Status)
	}
	if meta.Get("beta").Status == sshconfig.StatusChecking {
		t.Fatal("archived host was included in the startup check")
	}
}
