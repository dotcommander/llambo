package styles

import "github.com/charmbracelet/lipgloss"

// Console output styles
var (
	Dim     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	Warning = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Success = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	Error   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	Info    = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	Header  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
)
