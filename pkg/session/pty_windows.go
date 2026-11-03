//go:build windows

package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

type windowsPTY struct {
	cpty *conpty.ConPty
}

func (w *windowsPTY) Read(p []byte) (int, error) {
	return w.cpty.Read(p)
}

func (w *windowsPTY) Write(p []byte) (int, error) {
	return w.cpty.Write(p)
}

func (w *windowsPTY) Close() error {
	pid := w.cpty.Pid()
	if pid > 0 {
		// Force kill process tree immediately to guarantee SSH session disconnection
		_ = exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T", "/F").Run()
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
	return w.cpty.Close()
}

func (w *windowsPTY) Resize(cols, rows int) error {
	return w.cpty.Resize(cols, rows)
}

func (w *windowsPTY) Wait() (int, error) {
	code, err := w.cpty.Wait(context.Background())
	return int(code), err
}

// StartPTY spawns a process inside a Windows ConPTY pseudoconsole with UTF-8 encoding.
func StartPTY(command string, args []string, cols, rows int) (PTYDevice, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("Windows ConPTY is not available on this system")
	}

	// Ensure caller process console is configured for UTF-8
	_ = windows.SetConsoleCP(65001)
	_ = windows.SetConsoleOutputCP(65001)

	exe := command
	if lp, err := exec.LookPath(command); err == nil {
		exe = lp
	}

	cmdElements := []string{windows.EscapeArg(exe)}
	for _, a := range args {
		cmdElements = append(cmdElements, windows.EscapeArg(a))
	}
	cmdLine := strings.Join(cmdElements, " ")

	env := os.Environ()
	// Filter out any existing conflicting LANG/LC_* from Windows env
	var filteredEnv []string
	for _, e := range env {
		u := strings.ToUpper(e)
		if !strings.HasPrefix(u, "LANG=") &&
			!strings.HasPrefix(u, "LC_") &&
			!strings.HasPrefix(u, "TERM=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	filteredEnv = append(filteredEnv,
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"LC_CTYPE=en_US.UTF-8",
	)

	cpty, err := conpty.Start(
		cmdLine,
		conpty.ConPtyDimensions(cols, rows),
		conpty.ConPtyEnv(filteredEnv),
	)
	if err != nil {
		return nil, err
	}

	return &windowsPTY{cpty: cpty}, nil
}
