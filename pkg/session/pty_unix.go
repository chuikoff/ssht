//go:build !windows

package session

import (
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

type unixPTY struct {
	file *os.File
	cmd  *exec.Cmd
}

func (u *unixPTY) Read(p []byte) (int, error) {
	return u.file.Read(p)
}

func (u *unixPTY) Write(p []byte) (int, error) {
	return u.file.Write(p)
}

func (u *unixPTY) Close() error {
	_ = u.file.Close()
	if u.cmd != nil && u.cmd.Process != nil {
		_ = u.cmd.Process.Kill()
	}
	return nil
}

func (u *unixPTY) Resize(cols, rows int) error {
	return pty.Setsize(u.file, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (u *unixPTY) Wait() (int, error) {
	if u.cmd != nil && u.cmd.Process != nil {
		state, err := u.cmd.Process.Wait()
		if err != nil {
			return -1, err
		}
		return state.ExitCode(), nil
	}
	return 0, nil
}

// StartPTY spawns a process attached to a Unix PTY.
func StartPTY(command string, args []string, cols, rows int) (PTYDevice, error) {
	cmd := exec.Command(command, args...)
	env := os.Environ()
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
	cmd.Env = filteredEnv

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, err
	}

	return &unixPTY{file: f, cmd: cmd}, nil
}
