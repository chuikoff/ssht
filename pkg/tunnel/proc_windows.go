//go:build windows

package tunnel

import "golang.org/x/sys/windows"

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	h, err := windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}
