package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// filePalette is the ordered set of colours assigned to tailed files.
var filePalette = []color.Color{
	lipgloss.Color("214"), // orange
	lipgloss.Color("82"),  // green
	lipgloss.Color("39"),  // sky blue
	lipgloss.Color("207"), // pink
	lipgloss.Color("196"), // red
	lipgloss.Color("226"), // yellow
	lipgloss.Color("51"),  // cyan
	lipgloss.Color("141"), // purple
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
