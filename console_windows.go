//go:build windows

package main

import "golang.org/x/sys/windows"

func initConsoleUTF8() {
	_ = windows.SetConsoleCP(65001)
	_ = windows.SetConsoleOutputCP(65001)
}
