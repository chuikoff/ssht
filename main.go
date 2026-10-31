package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"sshtunnels/pkg/backup"
	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tui"
	"sshtunnels/pkg/tunnel"
)

const version = "1.1.0"

func truncateString(s string, maxLen int) string {
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-1]) + "…"
	}
	return s
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			fmt.Printf("SSHT — Терминальный менеджер SSH-подключений и тоннелей v%s\n", version)
			return

		case "-h", "--help":
			fmt.Printf("SSHT — Терминальный менеджер SSH-подключений и тоннелей v%s\n", version)
			fmt.Println("\nИспользование:")
			fmt.Println("  ssht               Запуск интерактивного TUI-дашборда")
			fmt.Println("  ssht daemon        Фоновый демон (автозапуск тоннелей и мониторинг)")
			fmt.Println("  ssht start-all     Запустить все сохраненные тоннели")
			fmt.Println("  ssht stop-all      Остановить все запущенные тоннели")
			fmt.Println("  ssht status        Показать статус всех тоннелей")
			fmt.Println("  ssht export [файл] Экспорт всех настроек в JSON-файл")
			fmt.Println("  ssht import <файл> Импорт настроек из резервной копии")
			fmt.Println("  ssht -v, --version Показать версию")
			fmt.Println("  ssht -h, --help    Показать справку")
			return

		case "daemon":
			manager, err := tunnel.NewManager()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("SSHT Daemon v%s запущен.\n", version)
			errs := manager.StartAutoStart()
			autoCount := 0
			for _, t := range manager.GetTunnels() {
				if t.AutoStart {
					autoCount++
				}
			}
			fmt.Printf("✔ Запущено тоннелей с автозапуском: %d (ошибок: %d)\n", autoCount, len(errs))
			for _, err := range errs {
				fmt.Fprintf(os.Stderr, "  • Ошибка запуска: %v\n", err)
			}
			fmt.Println("Фоновый сторож активен (авто-переподключение включено). Нажмите Ctrl+C для завершения.")

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			<-sigChan
			fmt.Println("\nОстановка тоннелей...")
			manager.StopAll()
			fmt.Println("✔ Демон остановлен.")
			return

		case "start-all":
			manager, err := tunnel.NewManager()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			tunnels := manager.GetTunnels()
			if len(tunnels) == 0 {
				fmt.Println("Список тоннелей пуст. Создайте тоннели в TUI командой 'ssht'.")
				return
			}
			fmt.Printf("Запуск %d тоннелей...\n", len(tunnels))
			errs := manager.StartAll()
			time.Sleep(1 * time.Second)
			for _, t := range manager.GetTunnels() {
				statusStr := "❌ ОШИБКА"
				if t.Status == tunnel.StatusActive {
					statusStr = fmt.Sprintf("✔ АКТИВЕН (PID %d)", t.PID)
				}
				fmt.Printf("  • [%s] %s (%s): %s\n", t.Type, t.Name, t.ForwardSpec(), statusStr)
				if t.LastError != "" {
					fmt.Printf("    Ошибка: %s\n", t.LastError)
				}
			}
			if len(errs) > 0 {
				fmt.Printf("Завершено с ошибками (%d/%d).\n", len(errs), len(tunnels))
			} else {
				fmt.Println("✔ Все тоннели успешно запущены.")
			}
			return

		case "stop-all":
			manager, err := tunnel.NewManager()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			manager.StopAll()
			fmt.Println("✔ Все активные тоннели остановлены.")
			return

		case "status":
			manager, err := tunnel.NewManager()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			tunnels := manager.GetTunnels()
			if len(tunnels) == 0 {
				fmt.Println("Список тоннелей пуст.")
				return
			}
			fmt.Printf("Всего тоннелей: %d (Активных: %d)\n\n", len(tunnels), manager.ActiveCount())
			fmt.Printf("%-20s %-8s %-25s %-15s %-10s %-15s\n", "НАЗВАНИЕ", "ТИП", "ПРАВИЛО", "ХОСТ", "АВТОПОВТОР", "СТАТУС")
			fmt.Println(strings.Repeat("─", 88))
			for _, t := range tunnels {
				statusStr := "Неактивен"
				if t.Status == tunnel.StatusActive {
					statusStr = fmt.Sprintf("Активен (%d)", t.PID)
				} else if t.Status == tunnel.StatusFailed {
					statusStr = "Ошибка"
				}
				autoRec := "Нет"
				if t.AutoReconnect {
					autoRec = "Да"
				}
				fmt.Printf("%-20s %-8s %-25s %-15s %-10s %-15s\n",
					truncateString(t.Name, 19),
					t.Type,
					truncateString(t.ForwardSpec(), 24),
					truncateString(t.Host, 14),
					autoRec,
					statusStr,
				)
			}
			return

		case "export":
			manager, err := tunnel.NewManager()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			metaStore, err := sshconfig.NewMetadataStore()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			target := ""
			if len(os.Args) > 2 {
				target = os.Args[2]
			}
			path, err := backup.Export(target, manager, metaStore)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка экспорта: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✔ Конфигурация успешно экспортирована в: %s\n", path)
			return

		case "import":
			if len(os.Args) < 3 {
				fmt.Println("Использование: ssht import <путь_к_файлу.json>")
				os.Exit(1)
			}
			manager, err := tunnel.NewManager()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			metaStore, err := sshconfig.NewMetadataStore()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
				os.Exit(1)
			}
			res, err := backup.Import(os.Args[2], manager, metaStore, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Ошибка импорта: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✔ Настройки успешно импортированы:\n  • Добавлено тоннелей: %d\n  • Добавлено хостов в ~/.ssh/config: %d\n", res.TunnelsImported, res.HostsAdded)
			return
		}
	}

	manager, err := tunnel.NewManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing tunnel manager: %v\n", err)
		os.Exit(1)
	}

	metaStore, err := sshconfig.NewMetadataStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing metadata store: %v\n", err)
		os.Exit(1)
	}

	// Trap termination signals to cleanly shut down any active tunnels
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		manager.StopAll()
		os.Exit(0)
	}()

	app := tui.NewAppModel(manager, metaStore)
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		manager.StopAll()
		os.Exit(1)
	}

	if appM, ok := finalModel.(*tui.AppModel); ok && appM.Detached {
		active := manager.ActiveCount()
		if active > 0 {
			fmt.Printf("SSHT переведен в фоновый режим. Активных тоннелей: %d.\nДля управления используйте 'ssht status' или 'ssht stop-all'.\n", active)
		}
		return
	}

	// Ensure all tunnels are cleanly stopped on normal exit
	manager.StopAll()
}
