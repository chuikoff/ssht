//go:build !windows

package tui

import "os/exec"

func launchUpdate() error {
	cmd := exec.Command("bash", "-lc", `curl -fsSL https://raw.githubusercontent.com/chuikoff/ssht/main/install/unix.sh | FORCE_INSTALL=true bash`)
	return cmd.Start()
}
