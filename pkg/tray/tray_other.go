//go:build !windows

package tray

import (
	"fmt"
	"sshtunnels/pkg/tunnel"
)

// GenerateTrayIcon is a stub for non-Windows platforms.
func GenerateTrayIcon() []byte {
	return nil
}

// Run is a stub for non-Windows platforms where native CGO is not enabled.
func Run(manager *tunnel.Manager) {
	fmt.Println("Иконка в системном трее поддерживается на платформе Windows.")
	fmt.Println("Для фоновой работы используйте 'ssht daemon'.")
}
