//go:build windows

package session

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/UserExistsError/conpty"
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

// StartPTY spawns a process inside a Windows ConPTY pseudoconsole.
func StartPTY(command string, args []string, cols, rows int) (PTYDevice, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("Windows ConPTY is not available on this system")
	}

	cmdLine := command
	if len(args) > 0 {
		cmdLine = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}

	// Resolve executable path if possible
	if lp, err := exec.LookPath(command); err == nil {
		if len(args) > 0 {
			cmdLine = fmt.Sprintf("\"%s\" %s", lp, strings.Join(args, " "))
		} else {
			cmdLine = fmt.Sprintf("\"%s\"", lp)
		}
	}

	env := os.Environ()
	// Ensure TERM is set so SSH and remote tools render full 256color ANSI
	env = append(env, "TERM=xterm-256color")

	cpty, err := conpty.Start(
		cmdLine,
		conpty.ConPtyDimensions(cols, rows),
		conpty.ConPtyEnv(env),
	)
	if err != nil {
		return nil, err
	}

	return &windowsPTY{cpty: cpty}, nil
}
