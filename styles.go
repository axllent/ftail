package main

import "github.com/charmbracelet/lipgloss"

// filePalette is the ordered set of colours assigned to tailed files.
var filePalette = []lipgloss.Color{
	"214", // orange
	"82",  // green
	"39",  // sky blue
	"207", // pink
	"196", // red
	"226", // yellow
	"51",  // cyan
	"141", // purple
}

var (
	searchBarStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	matchStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	fileStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ruleFollowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // green — following
	ruleScrollStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange — scrolled
	saveStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	saveMsgOkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	saveMsgErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	reStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13")) // magenta — regex mode
	reErrStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))  // red — invalid regex
	cursorStyle     = lipgloss.NewStyle().Reverse(true)
)
