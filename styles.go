package main

import "github.com/charmbracelet/lipgloss"

var (
	searchBarStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	matchStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	fileStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ruleFollowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // green — following
	ruleScrollStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange — scrolled
	saveStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	saveMsgOkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	saveMsgErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)
