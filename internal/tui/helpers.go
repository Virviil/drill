package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Spread places left and right on one line of width w.
func Spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// Clip shortens plain text to w runes with an ellipsis.
func Clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		return string(r[:max(0, w-1)]) + "…"
	}
	return s
}

// Truncate cuts styled text to w cells.
func Truncate(s string, w int) string { return ansi.Truncate(s, w, "") }

// IsKey reports whether msg is one of the keys.
func IsKey(msg tea.Msg, keys ...string) bool {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	s := k.String()
	for _, want := range keys {
		if s == want {
			return true
		}
	}
	return false
}

// MoveCursor handles j/k/up/down/g/G/pgup/pgdown for a list of n items.
func MoveCursor(msg tea.Msg, cur, n int) int {
	switch {
	case IsKey(msg, "j", "down"):
		cur++
	case IsKey(msg, "k", "up"):
		cur--
	case IsKey(msg, "g", "home"):
		cur = 0
	case IsKey(msg, "G", "end"):
		cur = n - 1
	case IsKey(msg, "pgdown", "ctrl+d"):
		cur += 10
	case IsKey(msg, "pgup", "ctrl+u"):
		cur -= 10
	}
	return max(0, min(cur, n-1))
}

// SplitRunes turns a multi-rune key message (fast typing, scripts) into
// single-rune messages. ok is false when msg needs no splitting.
func SplitRunes(msg tea.Msg) (keys []tea.KeyMsg, ok bool) {
	k, isKey := msg.(tea.KeyMsg)
	if !isKey || k.Type != tea.KeyRunes || len(k.Runes) < 2 || k.Paste {
		return nil, false
	}
	for _, r := range k.Runes {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return keys, true
}

// Bar is a text progress bar.
func Bar(n, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := min(width, n*width/total)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// Wrap breaks text at spaces so lines fit width runes.
func Wrap(s string, width int) string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			if line == "" {
				line = w
			} else if len([]rune(line))+1+len([]rune(w)) <= width {
				line += " " + w
			} else {
				out = append(out, line)
				line = w
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// PadBlock makes s exactly w×h cells.
func PadBlock(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lw := lipgloss.Width(l)
		if lw < w {
			lines[i] = l + strings.Repeat(" ", w-lw)
		} else if lw > w {
			lines[i] = Truncate(l, w)
		}
	}
	return strings.Join(lines, "\n")
}

// Frame draws the standard screen: header (logo, place, right-hand text),
// body, optional error line and a help line, fitted to w×h.
func Frame(st Styles, w, h int, place, right, body, errText, help string) string {
	title := st.Logo.Render(" DRILL ") + " " + st.Dim.Render(place)
	if right != "" {
		title = Spread(title, right, w)
	}
	lines := []string{title, st.Rule.Render(strings.Repeat("─", max(0, w)))}
	lines = append(lines, strings.Split(body, "\n")...)
	footer := 1
	if errText != "" {
		footer = 2
	}
	for len(lines) < h-footer {
		lines = append(lines, "")
	}
	if len(lines) > h-footer {
		lines = lines[:max(0, h-footer)]
	}
	if errText != "" {
		lines = append(lines, st.Error.Render(Clip(errText, w)))
	}
	lines = append(lines, st.Dim.Render(Clip(help, w)))
	return strings.Join(lines, "\n")
}
