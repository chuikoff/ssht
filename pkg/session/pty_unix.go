//go:build !windows

package session

import (
	"os"
	"os/exec"

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

// StartPTY spawns a process attached to a Unix PTY.
func StartPTY(command string, args []string, cols, rows int) (PTYDevice, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, err
	}

	return &unixPTY{file: f, cmd: cmd}, nil
}
