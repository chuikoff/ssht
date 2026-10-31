package tray

import (
	"testing"
)

func TestGenerateTrayIcon(t *testing.T) {
	icon := GenerateTrayIcon()
	if len(icon) <= 22 {
		t.Fatalf("Icon too small: %d bytes", len(icon))
	}
	if icon[2] != 1 || icon[4] != 1 {
		t.Fatalf("Invalid ICO header")
	}
}
