package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"sshtunnels/pkg/backup"
	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tui"
	"sshtunnels/pkg/tunnel"
)

const version = "1.0.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			fmt.Printf("SSHT — Терминальный менеджер SSH-подключений и тоннелей v%s\n", version)
			return
		case "-h", "--help":
			fmt.Println("SSHT — Терминальный менеджер SSH-подключений и тоннелей")
			fmt.Println("\nИспользование:")
			fmt.Println("  ssht               Запуск интерактивного TUI-дашборда")
			fmt.Println("  ssht export [файл] Экспорт всех настроек в JSON-файл")
			fmt.Println("  ssht import <файл> Импорт настроек из резервной копии")
			fmt.Println("  ssht -v            Показать версию")
			fmt.Println("  ssht -h            Показать справку")
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

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		manager.StopAll()
		os.Exit(1)
	}

	// Ensure all tunnels are cleanly stopped on normal exit
	manager.StopAll()
}
