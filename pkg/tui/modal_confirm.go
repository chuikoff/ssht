package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/sshconfig"
)

// ConfirmDeleteModal displays a confirmation dialog before deleting hosts or tunnels.
type ConfirmDeleteModal struct {
	Title      string
	Prompt     string
	IsTunnel   bool
	HostTarget *sshconfig.HostConfig
	TunnelID   string
	TunnelName string
}

func (m *ConfirmDeleteModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (confirmed bool, closed bool) {
	if msg.Type != tea.MouseLeft {
		return false, false
	}
	// Center calculation
	modalWidth := 56
	modalHeight := 8
	startX := (screenWidth - modalWidth) / 2
	startY := (screenHeight - modalHeight) / 2

	// Inside buttons line: startY + 4
	if msg.Y == startY+4 {
		// " [y] Да, удалить " button is approx startX+4 to startX+20
		if msg.X >= startX+4 && msg.X <= startX+22 {
			return true, true
		}
		// " [Esc] Отмена " button is approx startX+26 to startX+42
		if msg.X >= startX+24 && msg.X <= startX+44 {
			return false, true
		}
	}
	return false, false
}

func (m *ConfirmDeleteModal) View() string {
	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render("  " + m.Title + "  "))
	sb.WriteString("\n\n")

	promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	sb.WriteString(promptStyle.Render(m.Prompt))
	sb.WriteString("\n\n")

	btnYes := lipgloss.NewStyle().Foreground(ErrorColor).Bold(true).Render(" [y/Enter] Да, удалить ")
	btnNo := lipgloss.NewStyle().Foreground(TextMuted).Render(" [Esc/n] Отмена ")
	sb.WriteString(fmt.Sprintf("%s    %s\n", btnYes, btnNo))

	return ModalContainerStyle.Render(sb.String())
}
