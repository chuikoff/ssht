package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/sshconfig"
)

type RenameModal struct {
	Host       *sshconfig.HostConfig
	Input      textinput.Model
	ErrMessage string
}

func NewRenameModal(host *sshconfig.HostConfig) *RenameModal {
	ti := textinput.New()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(AccentColor)
	ti.CharLimit = 60
	ti.Placeholder = "например: prod-server, main-vps, db-primary"

	custom := host.CustomName()
	if custom != "" {
		ti.SetValue(custom)
	}
	ti.Focus()

	return &RenameModal{
		Host:  host,
		Input: ti,
	}
}

func (m *RenameModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (string, bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return "", false
	}

	modalHeight := 9
	modalWidth := 64
	topY := (screenHeight - modalHeight) / 2
	leftX := (screenWidth - modalWidth) / 2

	if msg.Y == topY+4 {
		m.Input.Focus()
		return "", false
	}

	if msg.Y >= topY+modalHeight-2 {
		relX := msg.X - leftX
		if relX > 10 && relX < 36 {
			alias := strings.TrimSpace(m.Input.Value())
			if alias == "" {
				m.ErrMessage = "Псевдоним не может быть пустым"
				return "", false
			}
			return alias, true
		}
		if relX >= 36 && relX < 58 {
			return "", true
		}
	}

	return "", false
}

func (m *RenameModal) Update(msg tea.Msg) (string, bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return "", true, nil
		case "enter":
			alias := strings.TrimSpace(m.Input.Value())
			if alias == "" {
				m.ErrMessage = "Псевдоним не может быть пустым"
				return "", false, nil
			}
			return alias, true, nil
		}
	}

	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	return "", false, cmd
}

func (m *RenameModal) View() string {
	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render("  ПЕРЕИМЕНОВАНИЕ SSH ПОДКЛЮЧЕНИЯ (ПСЕВДОНИМ)  "))
	sb.WriteString("\n\n")

	infoStyle := lipgloss.NewStyle().Foreground(TextMuted)
	valStyle := lipgloss.NewStyle().Foreground(TextLight).Bold(true)

	sb.WriteString(infoStyle.Render("IP / Хост:        ") + valStyle.Render(m.Host.HostName) + "\n")
	sb.WriteString(infoStyle.Render("Новый псевдоним:  ") + m.Input.View() + "\n")

	if m.ErrMessage != "" {
		sb.WriteString("\n" + ErrorNotificationStyle.Render("Ошибка: "+m.ErrMessage) + "\n")
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(
		"[Enter/Клик] Сохранить псевдоним   [Esc/Клик] Отмена",
	))

	return ModalContainerStyle.Render(sb.String())
}
