package tunnel

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens the specified URL in the system's default browser.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// Use rundll32 or cmd /c start
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}
