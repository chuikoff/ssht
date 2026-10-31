package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Palette
	PrimaryColor   = lipgloss.Color("#7D56F4") // Purple / Violet
	SecondaryColor = lipgloss.Color("#04B575") // Emerald Green
	AccentColor    = lipgloss.Color("#38BDF8") // Sky Blue
	WarningColor   = lipgloss.Color("#F59E0B") // Amber
	ErrorColor     = lipgloss.Color("#EF4444") // Coral Red
	SubtleColor    = lipgloss.Color("#64748B") // Slate Gray
	BgDark         = lipgloss.Color("#0F172A") // Dark Navy
	SurfaceDark    = lipgloss.Color("#1E293B") // Surface Slate
	TextLight      = lipgloss.Color("#F8FAFC") // Off-white
	TextMuted      = lipgloss.Color("#94A3B8") // Muted text

	// Header Styles
	AppTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextLight).
			Background(PrimaryColor).
			Padding(0, 1)

	ActiveCountBadge = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(SecondaryColor).
				Padding(0, 1).
				MarginLeft(1)

	InactiveCountBadge = lipgloss.NewStyle().
				Foreground(TextLight).
				Background(SubtleColor).
				Padding(0, 1).
				MarginLeft(1)

	// Tab Styles
	TabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(PrimaryColor).
			Padding(0, 2)

	TabInactiveStyle = lipgloss.NewStyle().
				Foreground(TextMuted).
				Background(SurfaceDark).
				Padding(0, 2)

	TabSeparatorStyle = lipgloss.NewStyle().
				Foreground(SubtleColor)

	// Tunnel Status Badges
	StatusActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#10B981")).
				SetString("● АКТИВЕН")

	StatusInactiveStyle = lipgloss.NewStyle().
				Foreground(TextMuted).
				SetString("○ ОСТАНОВЛЕН")

	StatusStartingStyle = lipgloss.NewStyle().
				Foreground(WarningColor).
				SetString("◐ ЗАПУСК...")

	StatusReconnectingStyle = lipgloss.NewStyle().
				Foreground(WarningColor).
				SetString("↻ ПЕРЕПОДКЛ...")

	StatusFailedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ErrorColor).
				SetString("✖ ОШИБКА")

	// Host Online Status Badges
	HostOnlineStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#10B981"))

	HostOfflineStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ErrorColor)

	HostCheckingStyle = lipgloss.NewStyle().
				Foreground(WarningColor)

	HostUnknownStyle = lipgloss.NewStyle().
				Foreground(SubtleColor)

	// Table Styles
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(AccentColor).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(SubtleColor)

	TableRowSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(TextLight).
				Background(lipgloss.Color("#334155"))

	TableRowNormalStyle = lipgloss.NewStyle().
				Foreground(TextLight)

	// Box / Container Styles
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(SubtleColor).
			Padding(0, 1)

	PanelActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(AccentColor).
				Padding(0, 1)

	// Modal / Dialog Styles
	ModalContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(PrimaryColor).
				Padding(1, 2).
				Background(SurfaceDark)

	ModalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(AccentColor).
			MarginBottom(1)

	// Footer / Keybind Bar
	FooterStyle = lipgloss.NewStyle().
			Foreground(TextMuted).
			Background(SurfaceDark).
			Padding(0, 1)

	KeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(AccentColor)

	DescStyle = lipgloss.NewStyle().
			Foreground(TextLight)

	NotificationStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(SecondaryColor).
				MarginLeft(1)

	ErrorNotificationStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ErrorColor).
				MarginLeft(1)
)

// RenderKeyHelp formats "[key] description".
func RenderKeyHelp(key, desc string) string {
	return KeyStyle.Render("["+key+"]") + " " + DescStyle.Render(desc)
}
