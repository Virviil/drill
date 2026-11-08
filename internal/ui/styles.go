package ui

import "github.com/charmbracelet/lipgloss"

type styles struct {
	Logo, Title, Accent, Dim, Rule, Error, OK, Warn, Selected, Box, Key lipgloss.Style
	Easy, Medium, Hard                                                 lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	ad := func(light, dark string) lipgloss.AdaptiveColor { return lipgloss.AdaptiveColor{Light: light, Dark: dark} }
	ns := r.NewStyle
	return styles{
		Logo:     ns().Bold(true).Background(ad("#1e66f5", "#61afef")).Foreground(ad("#ffffff", "#000000")),
		Title:    ns().Bold(true).Foreground(ad("#1e66f5", "#61afef")),
		Accent:   ns().Bold(true).Foreground(ad("#fe640b", "#e5c07b")),
		Dim:      ns().Foreground(ad("#8c8fa1", "#7f848e")),
		Rule:     ns().Foreground(ad("#ccd0da", "#3e4451")),
		Error:    ns().Foreground(ad("#d20f39", "#e06c75")),
		OK:       ns().Bold(true).Foreground(ad("#40a02b", "#98c379")),
		Warn:     ns().Foreground(ad("#df8e1d", "#d19a66")),
		Selected: ns().Bold(true).Foreground(ad("#000000", "#ffffff")).Background(ad("#dce0e8", "#3e4451")),
		Box:      ns().Border(lipgloss.RoundedBorder()).BorderForeground(ad("#ccd0da", "#3e4451")).Padding(0, 1),
		Key:      ns().Bold(true).Foreground(ad("#8839ef", "#c678dd")),
		Easy:     ns().Foreground(ad("#40a02b", "#98c379")),
		Medium:   ns().Foreground(ad("#df8e1d", "#e5c07b")),
		Hard:     ns().Foreground(ad("#d20f39", "#e06c75")),
	}
}

func (s styles) diff(d string) string {
	switch d {
	case "easy":
		return s.Easy.Render("Easy  ")
	case "medium":
		return s.Medium.Render("Medium")
	case "hard":
		return s.Hard.Render("Hard  ")
	}
	return d
}

const banner = `
 ██████╗ ██████╗ ██╗██╗     ██╗
 ██╔══██╗██╔══██╗██║██║     ██║
 ██║  ██║██████╔╝██║██║     ██║
 ██║  ██║██╔══██╗██║██║     ██║
 ██████╔╝██║  ██║██║███████╗███████╗
 ╚═════╝ ╚═╝  ╚═╝╚═╝╚══════╝╚══════╝`
