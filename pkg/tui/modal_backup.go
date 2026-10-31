package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/backup"
)

// ExportModal handles exporting configuration to a JSON file.
type ExportModal struct {
	Input      textinput.Model
	ErrMessage string
}

func NewExportModal() *ExportModal {
	ti := textinput.New()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(AccentColor)
	ti.CharLimit = 160
	ti.SetValue(backup.DefaultBackupPath())
	ti.Focus()

	return &ExportModal{
		Input: ti,
	}
}

func (m *ExportModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (string, bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return "", false
	}

	modalHeight := 9
	modalWidth := 74
	topY := (screenHeight - modalHeight) / 2
	leftX := (screenWidth - modalWidth) / 2

	if msg.Y == topY+3 {
		m.Input.Focus()
		return "", false
	}

	if msg.Y >= topY+modalHeight-2 {
		relX := msg.X - leftX
		if relX > 15 && relX < 45 {
			path := strings.TrimSpace(m.Input.Value())
			if path == "" {
				m.ErrMessage = "Укажите путь к файлу"
				return "", false
			}
			return path, true
		}
		if relX >= 45 && relX < 65 {
			return "", true // cancel
		}
	}

	return "", false
}

func (m *ExportModal) Update(msg tea.Msg) (string, bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return "", true, nil
		case "enter":
			path := strings.TrimSpace(m.Input.Value())
			if path == "" {
				m.ErrMessage = "Укажите путь к файлу"
				return "", false, nil
			}
			return path, true, nil
		}
	}

	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	return "", false, cmd
}

func (m *ExportModal) View() string {
	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render("  ЭКСПОРТ НАСТРОЕК В РЕЗЕРВНУЮ КОПИЮ  "))
	sb.WriteString("\n\n")

	sb.WriteString(lipgloss.NewStyle().Foreground(TextMuted).Render("Путь к файлу резервной копии:") + "\n")
	sb.WriteString(m.Input.View() + "\n")

	if m.ErrMessage != "" {
		sb.WriteString("\n" + ErrorNotificationStyle.Render("Ошибка: "+m.ErrMessage) + "\n")
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(
		"[Enter/Клик] Экспортировать   [Esc/Клик] Отмена",
	))

	return ModalContainerStyle.Render(sb.String())
}

// ImportModal handles importing configuration from a JSON file.
type ImportModal struct {
	Input      textinput.Model
	Overwrite  bool
	FocusIdx   int // 0: input, 1: toggle
	ErrMessage string
}

func NewImportModal() *ImportModal {
	ti := textinput.New()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(AccentColor)
	ti.CharLimit = 160
	ti.Placeholder = "Путь к файлу .json резервной копии"
	ti.Focus()

	return &ImportModal{
		Input:     ti,
		Overwrite: false,
		FocusIdx:  0,
	}
}

func (m *ImportModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (string, bool, bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return "", false, false
	}

	modalHeight := 11
	modalWidth := 74
	topY := (screenHeight - modalHeight) / 2
	leftX := (screenWidth - modalWidth) / 2

	if msg.Y == topY+3 {
		m.FocusIdx = 0
		m.Input.Focus()
		return "", false, false
	}

	if msg.Y == topY+5 {
		m.Overwrite = !m.Overwrite
		return "", false, false
	}

	if msg.Y >= topY+modalHeight-2 {
		relX := msg.X - leftX
		if relX > 15 && relX < 45 {
			path := strings.TrimSpace(m.Input.Value())
			if path == "" {
				m.ErrMessage = "Укажите путь к файлу"
				return "", false, false
			}
			return path, m.Overwrite, true
		}
		if relX >= 45 && relX < 65 {
			return "", false, true // cancel
		}
	}

	return "", false, false
}

func (m *ImportModal) Update(msg tea.Msg) (string, bool, bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return "", false, true, nil
		case "tab", "down", "up":
			if m.FocusIdx == 0 {
				m.FocusIdx = 1
				m.Input.Blur()
			} else {
				m.FocusIdx = 0
				m.Input.Focus()
			}
			return "", false, false, nil
		case " ", "left", "right":
			if m.FocusIdx == 1 {
				m.Overwrite = !m.Overwrite
				return "", false, false, nil
			}
		case "enter":
			path := strings.TrimSpace(m.Input.Value())
			if path == "" {
				m.ErrMessage = "Укажите путь к файлу"
				return "", false, false, nil
			}
			return path, m.Overwrite, true, nil
		}
	}

	if m.FocusIdx == 0 {
		var cmd tea.Cmd
		m.Input, cmd = m.Input.Update(msg)
		return "", false, false, cmd
	}

	return "", false, false, nil
}

func (m *ImportModal) View() string {
	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render("  ИМПОРТ / ВОССТАНОВЛЕНИЕ НАСТРОЕК  "))
	sb.WriteString("\n\n")

	sb.WriteString(lipgloss.NewStyle().Foreground(TextMuted).Render("Путь к файлу резервной копии:") + "\n")
	sb.WriteString(m.Input.View() + "\n\n")

	modeStr := "< Объединить (Merge) > (новые записи добавятся, текущие останутся)"
	if m.Overwrite {
		modeStr = "< Заменить всё (Overwrite) > (текущие тоннели будут заменены)"
	}

	modeLabel := lipgloss.NewStyle().Foreground(TextMuted).Render("Режим импорта:  ")
	if m.FocusIdx == 1 {
		sb.WriteString(modeLabel + lipgloss.NewStyle().Bold(true).Foreground(WarningColor).Render(modeStr) + "\n")
	} else {
		sb.WriteString(modeLabel + lipgloss.NewStyle().Foreground(TextLight).Render(modeStr) + "\n")
	}

	if m.ErrMessage != "" {
		sb.WriteString("\n" + ErrorNotificationStyle.Render("Ошибка: "+m.ErrMessage) + "\n")
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(
		"[Enter/Клик] Импортировать   [Пробел] Сменить режим   [Esc/Клик] Отмена",
	))

	return ModalContainerStyle.Render(sb.String())
}
