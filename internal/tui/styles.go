package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Palette — easy to tweak in one place.
	colorAccent  = lipgloss.Color("#7D56F4")
	colorMuted   = lipgloss.Color("#6B7280")
	colorErr     = lipgloss.Color("#EF4444")
	colorOK      = lipgloss.Color("#10B981")
	colorWarn    = lipgloss.Color("#F59E0B")
	colorDim     = lipgloss.Color("#374151")
	colorFg      = lipgloss.Color("#E5E7EB")
	colorBgPanel = lipgloss.Color("#111827")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	statusOK   = lipgloss.NewStyle().Foreground(colorOK)
	statusWarn = lipgloss.NewStyle().Foreground(colorWarn)
	statusErr  = lipgloss.NewStyle().Foreground(colorErr)
	muted      = lipgloss.NewStyle().Foreground(colorMuted)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1).
			MarginTop(1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	tabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 2).
			Border(lipgloss.Border{Bottom: "─"}, false, false, true, false).
			BorderForeground(colorAccent)

	tabInactive = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 2).
			Border(lipgloss.Border{Bottom: " "}, false, false, true, false).
			BorderForeground(colorDim)

	formLabel = lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	formInput = lipgloss.NewStyle().Foreground(colorFg)

	selectedRow = lipgloss.NewStyle().
			Foreground(colorFg).
			Background(colorBgPanel).
			Padding(0, 1)
	normalRow = lipgloss.NewStyle().Padding(0, 1)
)
