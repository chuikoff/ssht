//go:build windows

package tui

import (
	"os/exec"
	"syscall"
)

const updateCommand = `& ([scriptblock]::Create((Invoke-RestMethod -Uri 'https://raw.githubusercontent.com/chuikoff/ssht/main/install/windows.ps1'))) -Force`

func launchUpdate() error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", updateCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	return cmd.Start()
}
