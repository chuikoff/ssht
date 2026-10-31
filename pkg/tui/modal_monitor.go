package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/monitoring"
)

type MonitorModal struct {
	HostName   string
	Alias      string
	Metrics    *monitoring.ServerMetrics
	Loading    bool
	ErrMessage string
	Width      int
	Height     int
}

func NewMonitorModal(hostName, alias string) *MonitorModal {
	return &MonitorModal{
		HostName: hostName,
		Alias:    alias,
		Loading:  true,
	}
}

func (m *MonitorModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (action string, closed bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return "", false
	}

	modalHeight := 19
	modalWidth := 74
	topY := (screenHeight - modalHeight) / 2
	leftX := (screenWidth - modalWidth) / 2

	// Bottom action buttons zone
	if msg.Y >= topY+modalHeight-3 && msg.Y <= topY+modalHeight {
		relX := msg.X - leftX
		if relX >= 20 && relX <= 38 {
			return "refresh", false
		}
		if relX >= 40 && relX <= 60 {
			return "close", true
		}
	}

	// Click outside modal
	if msg.X < leftX || msg.X > leftX+modalWidth || msg.Y < topY || msg.Y > topY+modalHeight {
		return "close", true
	}

	return "", false
}

func renderBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int((percent / 100.0) * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	barStyle := lipgloss.NewStyle().Foreground(SecondaryColor)
	if percent >= 85 {
		barStyle = lipgloss.NewStyle().Foreground(ErrorColor)
	} else if percent >= 60 {
		barStyle = lipgloss.NewStyle().Foreground(WarningColor)
	}

	filledStr := barStyle.Render(strings.Repeat("█", filled))
	emptyStr := lipgloss.NewStyle().Foreground(SubtleColor).Render(strings.Repeat("░", empty))

	return "[" + filledStr + emptyStr + "]"
}

func formatMB(mb uint64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", float64(mb)/1024.0)
	}
	return fmt.Sprintf("%d MB", mb)
}

func (m *MonitorModal) View() string {
	var sb strings.Builder

	title := fmt.Sprintf("  МОНИТОРИНГ СЕРВЕРА: %s  ", m.HostName)
	sb.WriteString(ModalTitleStyle.Render(title))
	sb.WriteString("\n\n")

	if m.Loading {
		sb.WriteString(lipgloss.NewStyle().Foreground(AccentColor).Bold(true).Render("  ⣾ Опрос сервера через SSH (CPU, RAM, Диск, Uptime)..."))
		sb.WriteString("\n\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(TextMuted).Render("  Пожалуйста, подождите несколько секунд."))
		sb.WriteString("\n\n\n\n\n\n\n\n")
	} else if m.ErrMessage != "" {
		sb.WriteString(ErrorNotificationStyle.Render(fmt.Sprintf("Ошибка получения метрик:\n%s", m.ErrMessage)))
		sb.WriteString("\n\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(TextMuted).Render("Убедитесь, что сервер включен и доступен по SSH (проверьте ключ и сеть)."))
		sb.WriteString("\n\n\n\n\n")
	} else if m.Metrics != nil {
		met := m.Metrics

		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
		valStyle := lipgloss.NewStyle().Foreground(TextLight)
		badgeStyle := lipgloss.NewStyle().Foreground(AccentColor).Bold(true)

		// 1. System & OS
		sb.WriteString(labelStyle.Render("🖥  СИСТЕМА:    "))
		sb.WriteString(valStyle.Render(met.OS))
		sb.WriteString("\n")

		sb.WriteString(labelStyle.Render("⏱  UPTIME:     "))
		sb.WriteString(valStyle.Render(met.Uptime))
		sb.WriteString("\n")

		sb.WriteString(labelStyle.Render("⚖  LOAD AVG:   "))
		sb.WriteString(fmt.Sprintf("%s  %s  %s (1m, 5m, 15m)\n\n",
			badgeStyle.Render(met.Load1),
			badgeStyle.Render(met.Load5),
			badgeStyle.Render(met.Load15),
		))

		// 2. CPU
		sb.WriteString(labelStyle.Render("⚡ CPU:        "))
		sb.WriteString(fmt.Sprintf("%s  %5.1f%%\n\n", renderBar(met.CPUPercent, 28), met.CPUPercent))

		// 3. RAM
		sb.WriteString(labelStyle.Render("🧠 ОЗУ (RAM):  "))
		sb.WriteString(fmt.Sprintf("%s  %5.1f%% (%s / %s)\n",
			renderBar(met.RAMPercent, 28),
			met.RAMPercent,
			formatMB(met.RAMUsedMB),
			formatMB(met.RAMTotalMB),
		))
		if met.SwapTotalMB > 0 {
			swapPct := float64(met.SwapUsedMB) / float64(met.SwapTotalMB) * 100
			sb.WriteString(labelStyle.Render("   SWAP:       "))
			sb.WriteString(fmt.Sprintf("%s  %5.1f%% (%s / %s)\n",
				renderBar(swapPct, 28),
				swapPct,
				formatMB(met.SwapUsedMB),
				formatMB(met.SwapTotalMB),
			))
		}
		sb.WriteString("\n")

		// 4. Disk
		sb.WriteString(labelStyle.Render("💾 ДИСК (/):   "))
		sb.WriteString(fmt.Sprintf("%s  %5.1f%% (%s / %s)\n\n",
			renderBar(met.DiskPercent, 28),
			met.DiskPercent,
			formatMB(met.DiskUsedMB),
			formatMB(met.DiskTotalMB),
		))

		timeStr := met.FetchedAt.Format("15:04:05")
		sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(fmt.Sprintf("Метрики получены в: %s\n", timeStr)))
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(
		"       [r / Клик] Обновить метрики         [Esc / Клик] Закрыть       ",
	))

	return ModalContainerStyle.Width(74).Render(sb.String())
}
