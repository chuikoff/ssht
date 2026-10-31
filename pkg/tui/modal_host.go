package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/sshconfig"
)

type HostModal struct {
	FocusIndex int
	Inputs     []textinput.Model
	ErrMessage string
}

const (
	fieldHostAlias = iota
	fieldHostName
	fieldHostUser
	fieldHostPort
	fieldHostKey
	hostFieldCount
)

func NewHostModal() *HostModal {
	inputs := make([]textinput.Model, hostFieldCount)

	for i := range inputs {
		ti := textinput.New()
		ti.Cursor.Style = lipgloss.NewStyle().Foreground(AccentColor)
		ti.CharLimit = 120
		inputs[i] = ti
	}

	inputs[fieldHostAlias].Placeholder = "например: myserver, dev-vps"
	inputs[fieldHostName].Placeholder = "IP-адрес или домен (например: 192.168.1.100)"
	inputs[fieldHostUser].Placeholder = "root (или ваше имя пользователя)"
	inputs[fieldHostPort].Placeholder = "22"
	inputs[fieldHostKey].Placeholder = "~/.ssh/id_ed25519 (путь к приватному ключу)"

	inputs[fieldHostUser].SetValue("root")
	inputs[fieldHostPort].SetValue("22")

	inputs[0].Focus()

	return &HostModal{
		FocusIndex: 0,
		Inputs:     inputs,
	}
}

func (m *HostModal) setFocus(idx int) {
	if idx < 0 || idx >= hostFieldCount {
		return
	}
	m.Inputs[m.FocusIndex].Blur()
	m.FocusIndex = idx
	m.Inputs[m.FocusIndex].Focus()
}

func (m *HostModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (*sshconfig.HostConfig, bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}

	modalHeight := 12
	modalWidth := 74
	topY := (screenHeight - modalHeight) / 2
	leftX := (screenWidth - modalWidth) / 2

	relY := msg.Y - topY - 2
	if relY >= 0 && relY < hostFieldCount {
		m.setFocus(relY)
		return nil, false
	}

	if msg.Y >= topY+modalHeight-3 && msg.Y <= topY+modalHeight {
		relX := msg.X - leftX
		if relX > 30 && relX < 56 {
			h, err := m.Validate()
			if err != nil {
				m.ErrMessage = err.Error()
				return nil, false
			}
			return h, true
		}
		if relX >= 56 && relX < 72 {
			return nil, true
		}
	}

	return nil, false
}

func (m *HostModal) Update(msg tea.Msg) (*sshconfig.HostConfig, bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return nil, true, nil
		case "tab", "down":
			m.Inputs[m.FocusIndex].Blur()
			m.FocusIndex = (m.FocusIndex + 1) % hostFieldCount
			m.Inputs[m.FocusIndex].Focus()
			return nil, false, nil
		case "shift+tab", "up":
			m.Inputs[m.FocusIndex].Blur()
			m.FocusIndex--
			if m.FocusIndex < 0 {
				m.FocusIndex = hostFieldCount - 1
			}
			m.Inputs[m.FocusIndex].Focus()
			return nil, false, nil
		case "enter":
			host, err := m.Validate()
			if err != nil {
				m.ErrMessage = err.Error()
				return nil, false, nil
			}
			return host, true, nil
		}
	}

	var cmds []tea.Cmd
	for i := range m.Inputs {
		var cmd tea.Cmd
		m.Inputs[i], cmd = m.Inputs[i].Update(msg)
		cmds = append(cmds, cmd)
	}

	return nil, false, tea.Batch(cmds...)
}

func (m *HostModal) Validate() (*sshconfig.HostConfig, error) {
	alias := strings.TrimSpace(m.Inputs[fieldHostAlias].Value())
	if alias == "" {
		return nil, fmt.Errorf("укажите псевдоним хоста")
	}

	hostname := strings.TrimSpace(m.Inputs[fieldHostName].Value())
	if hostname == "" {
		hostname = alias
	}

	user := strings.TrimSpace(m.Inputs[fieldHostUser].Value())
	if user == "" {
		user = "root"
	}

	portStr := strings.TrimSpace(m.Inputs[fieldHostPort].Value())
	port := 22
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 || p > 65535 {
			return nil, fmt.Errorf("некорректный порт")
		}
		port = p
	}

	key := strings.TrimSpace(m.Inputs[fieldHostKey].Value())

	return &sshconfig.HostConfig{
		Aliases:      []string{alias},
		HostName:     hostname,
		User:         user,
		Port:         port,
		IdentityFile: key,
		Options:      make(map[string]string),
	}, nil
}

func (m *HostModal) View() string {
	labels := []string{
		"Псевдоним:    ",
		"Хост / IP:    ",
		"Пользователь: ",
		"Порт:         ",
		"SSH Ключ:     ",
	}

	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render("  ДОБАВЛЕНИЕ SSH ХОСТА В ~/.ssh/config  "))
	sb.WriteString("\n\n")

	for i := 0; i < hostFieldCount; i++ {
		isFocused := m.FocusIndex == i

		labelStyle := lipgloss.NewStyle().Foreground(TextMuted).Width(16)
		if isFocused {
			labelStyle = labelStyle.Bold(true).Foreground(AccentColor)
		}

		sb.WriteString(labelStyle.Render(labels[i]))
		sb.WriteString(m.Inputs[i].View())
		sb.WriteString("\n")
	}

	if m.ErrMessage != "" {
		sb.WriteString("\n")
		sb.WriteString(ErrorNotificationStyle.Render("Ошибка: " + m.ErrMessage))
		sb.WriteString("\n")
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(
		"[Tab/Вниз] След.  [Shift+Tab/Вверх] Пред.  [Enter/Клик] Сохранить в config  [Esc/Клик] Отмена",
	))

	return ModalContainerStyle.Render(sb.String())
}
