package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/tunnel"
)

type LogModal struct {
	Tunnel   *tunnel.Tunnel
	Viewport viewport.Model
	Content  string
}

func NewLogModal(t *tunnel.Tunnel, logs string, width, height int) *LogModal {
	vp := viewport.New(width-8, height-12)
	vp.Style = lipgloss.NewStyle().Foreground(TextLight)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Тоннель: %s\n", t.Name))
	sb.WriteString(fmt.Sprintf("Тип:     %s\n", strings.ToUpper(string(t.Type))))
	sb.WriteString(fmt.Sprintf("Правило: %s\n", t.Summary()))
	sb.WriteString(fmt.Sprintf("Хост:    %s\n", t.Host))
	sb.WriteString(fmt.Sprintf("Статус:  %s\n", t.Status))
	if t.PID > 0 {
		sb.WriteString(fmt.Sprintf("PID:     %d\n", t.PID))
	}
	sb.WriteString("\n--- Логи процесса / Вывод stderr ---\n")
	if strings.TrimSpace(logs) == "" {
		sb.WriteString("(Вывод stderr пуст)")
	} else {
		sb.WriteString(logs)
	}

	content := sb.String()
	vp.SetContent(content)

	return &LogModal{
		Tunnel:   t,
		Viewport: vp,
		Content:  content,
	}
}

func (m *LogModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) bool {
	if msg.Action != tea.MouseActionPress {
		return false
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.Viewport.LineUp(2)
		return false
	case tea.MouseButtonWheelDown:
		m.Viewport.LineDown(2)
		return false
	case tea.MouseButtonLeft:
		modalHeight := m.Viewport.Height + 6
		topY := (screenHeight - modalHeight) / 2
		if msg.Y >= topY+modalHeight-2 {
			return true
		}
	}
	return false
}

func (m *LogModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "enter":
			return true, nil
		}
	}

	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return false, cmd
}

func (m *LogModal) View() string {
	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render("  ЛОГИ И ДИАГНОСТИКА ТОННЕЛЯ  "))
	sb.WriteString("\n\n")
	sb.WriteString(m.Viewport.View())
	sb.WriteString("\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(
		"[Esc/q/Клик] Закрыть  [Вверх/Вниз/Колесико] Прокрутка",
	))

	return ModalContainerStyle.Render(sb.String())
}
