package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type HelpView struct {
	Viewport viewport.Model
	Ready    bool
	Width    int
	Height   int
}

func NewHelpView() *HelpView {
	return &HelpView{}
}

func (v *HelpView) HandleMouse(msg tea.MouseMsg) bool {
	if !v.Ready {
		return false
	}
	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			v.Viewport.LineUp(3)
			return true
		case tea.MouseButtonWheelDown:
			v.Viewport.LineDown(3)
			return true
		}
	}
	return false
}

func (v *HelpView) Update(msg tea.Msg) tea.Cmd {
	if !v.Ready {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			v.Viewport.LineUp(1)
			return nil
		case "down", "j":
			v.Viewport.LineDown(1)
			return nil
		case "pgup", "b":
			v.Viewport.HalfViewUp()
			return nil
		case "pgdown", "f", "space":
			v.Viewport.HalfViewDown()
			return nil
		case "home", "g":
			v.Viewport.GotoTop()
			return nil
		case "end", "G":
			v.Viewport.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	v.Viewport, cmd = v.Viewport.Update(msg)
	return cmd
}

func (v *HelpView) View(width, height int) string {
	vpWidth := width - 4
	if vpWidth < 40 {
		vpWidth = 40
	}
	vpHeight := height - 3
	if vpHeight < 5 {
		vpHeight = 5
	}

	if !v.Ready || v.Width != vpWidth || v.Height != vpHeight {
		v.Width = vpWidth
		v.Height = vpHeight
		v.Viewport = viewport.New(vpWidth, vpHeight)
		v.Viewport.Style = lipgloss.NewStyle().Foreground(TextLight)
		v.Viewport.SetContent(buildHelpContent(vpWidth))
		v.Ready = true
	}

	var sb strings.Builder
	sb.WriteString(v.Viewport.View())
	sb.WriteString("\r\n")

	scrollInfo := fmt.Sprintf("Прокрутка: %3.f%%  [↑/↓/Колесико] Листать  [Home/End] Начало/Конец", v.Viewport.ScrollPercent()*100)
	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).PaddingLeft(1).Render(scrollInfo))

	return sb.String()
}

func buildHelpContent(width int) string {
	var lines []string

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(AccentColor)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(WarningColor)
	descStyle := lipgloss.NewStyle().Foreground(TextLight)
	exampleStyle := lipgloss.NewStyle().Foreground(SecondaryColor)

	add := func(s string) {
		lines = append(lines, s)
	}

	add(titleStyle.Render("📖 СПРАВКА И РУКОВОДСТВО ПОЛЬЗОВАТЕЛЯ SSHT"))
	add("")

	// Section 1: Navigation
	add(sectionStyle.Render("══ 1. НАВИГАЦИЯ И ОБЩИЕ КЛАВИШИ ═════════════════════════════════════════"))
	add("  " + keyStyle.Render("[1] / [Tab]       ") + descStyle.Render("Вкладка «SSH Подключения»"))
	add("  " + keyStyle.Render("[2] / [Tab]       ") + descStyle.Render("Вкладка «SSH Тоннели»"))
	add("  " + keyStyle.Render("[3] / [?]         ") + descStyle.Render("Вкладка «Справка»"))
	add("  " + keyStyle.Render("[↑/↓] / [j/k]     ") + descStyle.Render("Перемещение по строкам таблицы / прокрутка"))
	add("  " + keyStyle.Render("[PgUp / PgDn]     ") + descStyle.Render("Постраничная прокрутка"))
	add("  " + keyStyle.Render("[/]               ") + descStyle.Render("Мгновенный поиск / фильтрация"))
	add("  " + keyStyle.Render("[x]               ") + descStyle.Render("Экспорт настроек в файл резервной копии"))
	add("  " + keyStyle.Render("[i]               ") + descStyle.Render("Импорт / восстановление настроек из файла"))
	add("  " + keyStyle.Render("[b]               ") + descStyle.Render("Выход в фон (оставить тоннели работать)"))
	add("  " + keyStyle.Render("[q] / [Ctrl+C]    ") + descStyle.Render("Выход с корректным завершением активных тоннелей"))
	add("")

	// Section 2: Mouse
	add(sectionStyle.Render("══ 2. УПРАВЛЕНИЕ МЫШЬЮ ═══════════════════════════════════════════════════"))
	add("  " + exampleStyle.Render("• Клик по вкладкам:") + descStyle.Render("  Переключение экранов (1. Подключения / 2. Тоннели / 3. Справка)"))
	add("  " + exampleStyle.Render("• Колесико мыши:   ") + descStyle.Render("  Плавная прокрутка списков серверов, тоннелей и справки"))
	add("  " + exampleStyle.Render("• Одиночный клик:  ") + descStyle.Render("  Выбор и подсветка строки в таблице"))
	add("  " + exampleStyle.Render("• Двойной клик:    ") + descStyle.Render("  Быстрое действие: вход по SSH (на вкладке 1) или Вкл/Выкл (на вкладке 2)"))
	add("  " + exampleStyle.Render("• Кнопки внизу:    ") + descStyle.Render("  Все функциональные кнопки панели действий кликабельны"))
	add("")

	// Section 3: SSH Connections
	add(sectionStyle.Render("══ 3. ДЕЙСТВИЯ: SSH ПОДКЛЮЧЕНИЯ ══════════════════════════════════════════"))
	add("  " + keyStyle.Render("[Enter]           ") + descStyle.Render("Подключиться к серверу (интерактивная SSH-сессия)"))
	add("  " + keyStyle.Render("[m]               ") + descStyle.Render("Мини-мониторинг сервера: CPU, ОЗУ, Диск, Uptime и Load Average"))
	add("  " + keyStyle.Render("[p] или [c]       ") + descStyle.Render("Проверить доступность (Ping) серверов параллельно"))
	add("  " + keyStyle.Render("[z]               ") + descStyle.Render("Отправить в «Отстойник» / Восстановить выбранный сервер"))
	add("  " + keyStyle.Render("[Z]               ") + descStyle.Render("Сменить фильтр списка: [Активные] / [Отстойник] / [Все]"))
	add("  " + keyStyle.Render("[n]               ") + descStyle.Render("Переименовать хост (задать удобный псевдоним в ~/.ssh/config)"))
	add("  " + keyStyle.Render("[a]               ") + descStyle.Render("Добавить новый SSH хост в ~/.ssh/config"))
	add("  " + keyStyle.Render("[t]               ") + descStyle.Render("Быстро создать тоннель для выбранного сервера"))
	add("  " + keyStyle.Render("[r]               ") + descStyle.Render("Перечитать конфигурацию ~/.ssh/config с диска"))
	add("")

	// Section 4: SSH Tunnels
	add(sectionStyle.Render("══ 4. ДЕЙСТВИЯ: SSH ТОННЕЛИ ══════════════════════════════════════════════"))
	add("  " + keyStyle.Render("[Пробел] / [Enter]") + descStyle.Render("Включить / Выключить тоннель (ON / OFF)"))
	add("  " + keyStyle.Render("[o]               ") + descStyle.Render("Открыть порт тоннеля в веб-браузере (http://localhost:порт)"))
	add("  " + keyStyle.Render("[a]               ") + descStyle.Render("Создать новый SSH тоннель (с поддержкой автоповтора)"))
	add("  " + keyStyle.Render("[e]               ") + descStyle.Render("Редактировать параметры выбранного тоннеля"))
	add("  " + keyStyle.Render("[d]               ") + descStyle.Render("Удалить выбранный тоннель"))
	add("  " + keyStyle.Render("[l]               ") + descStyle.Render("Просмотреть логи процесса / ошибки запуска (stderr)"))
	add("  " + keyStyle.Render("[s]               ") + descStyle.Render("Остановить сразу ВСЕ активные тоннели"))
	add("")

	// Section 5: Tunnel Types
	add(sectionStyle.Render("══ 5. ТИПЫ SSH ТОННЕЛЕЙ И ПРИМЕРЫ ════════════════════════════════════════"))
	add("  " + keyStyle.Render("1. Локальный (Local Forward, -L):"))
	add("     " + descStyle.Render("Пробрасывает порт вашего компьютера на удаленный сервис в сети сервера."))
	add("     " + exampleStyle.Render("Формула: ") + descStyle.Render("локальный_порт : удаленный_хост : удаленный_порт"))
	add("     " + exampleStyle.Render("Пример:  ") + descStyle.Render("5432:127.0.0.1:5432 — подключение к PostgreSQL через localhost:5432."))
	add("")
	add("  " + keyStyle.Render("2. Удаленный (Remote Forward, -R):"))
	add("     " + descStyle.Render("Публикует локальный сервис вашего компьютера на удаленном сервере."))
	add("     " + exampleStyle.Render("Формула: ") + descStyle.Render("порт_сервера : локальный_хост : локальный_порт"))
	add("     " + exampleStyle.Render("Пример:  ") + descStyle.Render("8080:127.0.0.1:3000 — открыть локальный dev-сервер наружу."))
	add("")
	add("  " + keyStyle.Render("3. Динамический (Dynamic SOCKS5, -D):"))
	add("     " + descStyle.Render("Создает защищенный SOCKS5-прокси через удаленный SSH-сервер."))
	add("     " + exampleStyle.Render("Формула: ") + descStyle.Render("локальный_порт"))
	add("     " + exampleStyle.Render("Пример:  ") + descStyle.Render("1080 — укажите 127.0.0.1:1080 в браузере или Telegram."))
	add("")

	// Section 6: Backup & Files
	add(sectionStyle.Render("══ 6. РЕЗЕРВНОЕ КОПИРОВАНИЕ И ФАЙЛЫ НАСТРОЕК ═════════════════════════════"))
	add("  " + keyStyle.Render("[x] Экспорт:      ") + descStyle.Render("Сохраняет все хосты, тоннели и историю в один JSON-файл."))
	add("  " + keyStyle.Render("[i] Импорт:       ") + descStyle.Render("Восстанавливает настройки с выбором режима (Объединить/Заменить)."))
	add("  " + exampleStyle.Render("• SSH хосты:     ") + descStyle.Render("~/.ssh/config (стандартный файл конфигурации OpenSSH)"))
	add("  " + exampleStyle.Render("• SSH тоннели:   ") + descStyle.Render("~/.ssh/ssht_tunnels.json (список правил и автозапуск)"))
	add("  " + exampleStyle.Render("• Метаданные:    ") + descStyle.Render("~/.ssh/ssht_metadata.json (история входов и статус)"))
	add("")

	// Section 7: Background & CLI
	add(sectionStyle.Render("══ 7. ФОНОВАЯ РАБОТА И CLI КОМАНДЫ ════════════════════════════════════════"))
	add("  " + keyStyle.Render("ssht              ") + descStyle.Render("Запуск полноэкранного интерактивного TUI"))
	add("  " + keyStyle.Render("ssht tray         ") + descStyle.Render("Иконка в системном трее Windows с меню и авто-мониторингом"))
	add("  " + keyStyle.Render("ssht daemon       ") + descStyle.Render("Фоновый сторож: автозапуск тоннелей и авто-переподключение"))
	add("  " + keyStyle.Render("ssht start-all    ") + descStyle.Render("Запустить все сохраненные тоннели из командной строки"))
	add("  " + keyStyle.Render("ssht stop-all     ") + descStyle.Render("Остановить все запущенные SSH-тоннели"))
	add("  " + keyStyle.Render("ssht status       ") + descStyle.Render("Вывести статус и PID активных тоннелей в консоль"))
	add("  " + exampleStyle.Render("• Автоповтор:    ") + descStyle.Render("При разрыве сети тоннель автоматически переподключается"))

	// Join lines with explicit CRLF so terminal in raw mode never drifts
	return strings.Join(lines, "\r\n")
}
