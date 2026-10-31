//go:build windows

package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os/exec"
	"runtime"
	"time"

	"github.com/energye/systray"
	"sshtunnels/pkg/tunnel"
)

// GenerateTrayIcon dynamically generates a 32x32 Windows ICO byte slice
// depicting a glowing terminal tunnel icon.
func GenerateTrayIcon() []byte {
	width := 32
	height := 32
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	bgColor := color.RGBA{R: 24, G: 28, B: 40, A: 255}
	accentColor := color.RGBA{R: 0, G: 230, B: 118, A: 255} // Emerald Green
	ringColor := color.RGBA{R: 0, G: 210, B: 255, A: 255}   // Cyan

	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// Draw outer cyan ring and inner emerald core (symbolizing secure tunnel)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dx := float64(x - 16)
			dy := float64(y - 16)
			dist := dx*dx + dy*dy
			if dist >= 100 && dist <= 144 {
				img.Set(x, y, ringColor)
			} else if dist <= 28 {
				img.Set(x, y, accentColor)
			}
		}
	}

	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	pngBytes := pngBuf.Bytes()
	size := len(pngBytes)

	// Wrap in standard Windows ICO structure
	ico := make([]byte, 22+size)
	ico[0] = 0 // reserved
	ico[1] = 0
	ico[2] = 1 // type 1 = ICO
	ico[3] = 0
	ico[4] = 1 // 1 image
	ico[5] = 0

	// ICONDIRENTRY (16 bytes)
	ico[6] = 32  // width
	ico[7] = 32  // height
	ico[8] = 0   // color count
	ico[9] = 0   // reserved
	ico[10] = 1  // planes
	ico[11] = 0
	ico[12] = 32 // bit count
	ico[13] = 0
	ico[14] = byte(size)
	ico[15] = byte(size >> 8)
	ico[16] = byte(size >> 16)
	ico[17] = byte(size >> 24)
	ico[18] = 22 // offset
	ico[19] = 0
	ico[20] = 0
	ico[21] = 0

	copy(ico[22:], pngBytes)
	return ico
}

// Run launches the system tray loop in the current process.
func Run(manager *tunnel.Manager) {
	onReady := func() {
		iconBytes := GenerateTrayIcon()
		systray.SetIcon(iconBytes)
		systray.SetTitle("SSHT")
		systray.SetTooltip("SSHT: Терминальный менеджер SSH и тоннелей")

		mTitle := systray.AddMenuItem("SSHT — Терминальный менеджер", "SSHT v1.2.0")
		mTitle.Disable()

		mStatus := systray.AddMenuItem(fmt.Sprintf("Активных тоннелей: %d", manager.ActiveCount()), "Текущее количество активных тоннелей")
		mStatus.Disable()

		systray.AddSeparator()

		// Launch TUI button
		mOpenTUI := systray.AddMenuItem("💻 Открыть окно терминала (TUI)", "Запустить полноэкранный интерфейс SSHT")
		mOpenTUI.Click(func() {
			launchTerminal()
		})

		systray.AddSeparator()

		// Browser quick launch section
		tunnels := manager.GetTunnels()
		if len(tunnels) > 0 {
			mBrowser := systray.AddMenuItem("🌐 Открыть в браузере", "Быстрый переход к веб-интерфейсу тоннеля")
			for _, t := range tunnels {
				if t.BrowserURL() != "" {
					item := mBrowser.AddSubMenuItem(fmt.Sprintf("%s (%s)", t.Name, t.BrowserURL()), t.BrowserURL())
					targetTun := t
					item.Click(func() {
						if targetTun.Status != tunnel.StatusActive {
							_ = manager.StartTunnel(targetTun.ID)
						}
						_ = tunnel.OpenBrowser(targetTun.BrowserURL())
					})
				}
			}
			systray.AddSeparator()
		}

		// Controls
		mStartAll := systray.AddMenuItem("▶ Запустить все тоннели", "Запустить все сохраненные тоннели")
		mStartAll.Click(func() {
			_ = manager.StartAll()
		})

		mStopAll := systray.AddMenuItem("⏹ Остановить все тоннели", "Остановить все запущенные тоннели")
		mStopAll.Click(func() {
			manager.StopAll()
		})

		systray.AddSeparator()

		// Quit
		mQuit := systray.AddMenuItem("❌ Выход из SSHT", "Остановить тоннели и выйти")
		mQuit.Click(func() {
			manager.StopAll()
			systray.Quit()
		})

		// Periodic status updates
		go func() {
			for {
				time.Sleep(2 * time.Second)
				active := manager.ActiveCount()
				total := len(manager.GetTunnels())
				mStatus.SetTitle(fmt.Sprintf("Активных тоннелей: %d из %d", active, total))
				systray.SetTooltip(fmt.Sprintf("SSHT: Активно %d из %d тоннелей", active, total))
			}
		}()
	}

	onExit := func() {
		manager.StopAll()
	}

	systray.Run(onReady, onExit)
}

func launchTerminal() {
	if runtime.GOOS == "windows" {
		// Try Windows Terminal (wt), fallback to cmd
		err := exec.Command("cmd", "/c", "start", "wt", "ssht").Run()
		if err != nil {
			_ = exec.Command("cmd", "/c", "start", "powershell", "-NoExit", "-Command", "ssht").Run()
		}
	} else if runtime.GOOS == "darwin" {
		_ = exec.Command("open", "-a", "Terminal", "ssht").Run()
	} else {
		_ = exec.Command("x-terminal-emulator", "-e", "ssht").Run()
	}
}
