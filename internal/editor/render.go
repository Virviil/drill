package editor

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

const tabWidth = 4

// Styles used to draw the editor. Build them from the session's renderer.
type Styles struct {
	Text, Keyword, String, Number, Comment, Func, Type, Punct lipgloss.Style
	Gutter, GutterCur, Cursor, CursorInsert                   lipgloss.Style
	Status, StatusMode, StatusInsert, Msg                     lipgloss.Style
}

// DefaultStyles returns a palette that works on dark and light terminals.
func DefaultStyles(r *lipgloss.Renderer) Styles {
	ns := r.NewStyle
	ad := func(light, dark string) lipgloss.AdaptiveColor {
		return lipgloss.AdaptiveColor{Light: light, Dark: dark}
	}
	return Styles{
		Text:         ns(),
		Keyword:      ns().Foreground(ad("#8839ef", "#c678dd")),
		String:       ns().Foreground(ad("#40a02b", "#98c379")),
		Number:       ns().Foreground(ad("#d20f39", "#d19a66")),
		Comment:      ns().Foreground(ad("#8c8fa1", "#7f848e")).Italic(true),
		Func:         ns().Foreground(ad("#1e66f5", "#61afef")),
		Type:         ns().Foreground(ad("#df8e1d", "#e5c07b")),
		Punct:        ns().Foreground(ad("#5c5f77", "#abb2bf")),
		Gutter:       ns().Foreground(ad("#9ca0b0", "#5c6370")),
		GutterCur:    ns().Foreground(ad("#4c4f69", "#e5c07b")).Bold(true),
		Cursor:       ns().Reverse(true),
		CursorInsert: ns().Background(ad("#04a5e5", "#56b6c2")).Foreground(ad("#ffffff", "#000000")),
		Status:       ns().Foreground(ad("#4c4f69", "#abb2bf")),
		StatusMode:   ns().Bold(true).Padding(0, 1).Background(ad("#7287fd", "#61afef")).Foreground(ad("#ffffff", "#000000")),
		StatusInsert: ns().Bold(true).Padding(0, 1).Background(ad("#40a02b", "#98c379")).Foreground(ad("#ffffff", "#000000")),
		Msg:          ns().Foreground(ad("#d20f39", "#e06c75")),
	}
}

type tokKind uint8

const (
	kText tokKind = iota
	kKeyword
	kString
	kNumber
	kComment
	kFunc
	kType
	kPunct
)

type highlighter struct {
	lexer chroma.Lexer
}

func newHighlighter(lang string) *highlighter {
	l := lexers.Get(lang)
	if l == nil {
		return &highlighter{}
	}
	return &highlighter{lexer: chroma.Coalesce(l)}
}

// kinds returns a token kind for every rune of every line.
func (h *highlighter) kinds(lines [][]rune) [][]tokKind {
	out := make([][]tokKind, len(lines))
	for i, l := range lines {
		out[i] = make([]tokKind, len(l))
	}
	if h.lexer == nil {
		return out
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(string(l))
		b.WriteByte('\n')
	}
	it, err := h.lexer.Tokenise(nil, b.String())
	if err != nil {
		return out
	}
	row, col := 0, 0
	for _, tok := range it.Tokens() {
		k := kindOf(tok.Type)
		for _, r := range tok.Value {
			if r == '\n' {
				row++
				col = 0
				continue
			}
			if row < len(out) && col < len(out[row]) {
				out[row][col] = k
			}
			col++
		}
	}
	return out
}

func kindOf(t chroma.TokenType) tokKind {
	switch {
	case t.InCategory(chroma.Comment):
		return kComment
	case t == chroma.KeywordType || t == chroma.NameBuiltin || t == chroma.NameClass:
		return kType
	case t.InCategory(chroma.Keyword):
		return kKeyword
	case t.InCategory(chroma.LiteralString):
		return kString
	case t.InCategory(chroma.LiteralNumber):
		return kNumber
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return kFunc
	case t.InCategory(chroma.Operator) || t.InCategory(chroma.Punctuation):
		return kPunct
	}
	return kText
}

func (s Styles) of(k tokKind) lipgloss.Style {
	switch k {
	case kKeyword:
		return s.Keyword
	case kString:
		return s.String
	case kNumber:
		return s.Number
	case kComment:
		return s.Comment
	case kFunc:
		return s.Func
	case kType:
		return s.Type
	case kPunct:
		return s.Punct
	}
	return s.Text
}

// ---- geometry ----

func (e *Editor) textHeight() int { return max(1, e.height-1) }

func (e *Editor) gutterWidth() int { return len(fmt.Sprint(len(e.lines))) + 2 }

func (e *Editor) textWidth() int { return max(1, e.width-e.gutterWidth()) }

func dispWidth(r []rune, upto int) int {
	w := 0
	for i := 0; i < upto && i < len(r); i++ {
		if r[i] == '\t' {
			w += tabWidth - w%tabWidth
		} else {
			w++
		}
	}
	return w
}

func (e *Editor) dispCol() int { return dispWidth(e.cur(), e.col) }

func (e *Editor) colForDisp(want int) int {
	line := e.cur()
	w := 0
	for i, r := range line {
		next := w + 1
		if r == '\t' {
			next = w + tabWidth - w%tabWidth
		}
		if next > want {
			return i
		}
		w = next
	}
	if e.mode == Insert {
		return len(line)
	}
	return max(0, len(line)-1)
}

func (e *Editor) scroll() {
	h := e.textHeight()
	if e.row < e.top {
		e.top = e.row
	}
	if e.row >= e.top+h {
		e.top = e.row - h + 1
	}
	tw := e.textWidth()
	dc := e.dispCol()
	if dc < e.left {
		e.left = dc
	}
	if dc >= e.left+tw {
		e.left = dc - tw + 1
	}
}

// View draws exactly height lines of exactly width cells.
func (e *Editor) View() string {
	if e.width <= 0 || e.height <= 0 {
		return ""
	}
	kinds := e.hl.kinds(e.lines)
	gw := e.gutterWidth()
	tw := e.textWidth()
	var b strings.Builder
	for i := 0; i < e.textHeight(); i++ {
		row := e.top + i
		if row >= len(e.lines) {
			b.WriteString(e.styles.Gutter.Render(pad("~", gw)))
			b.WriteString(strings.Repeat(" ", tw))
			b.WriteByte('\n')
			continue
		}
		num := fmt.Sprintf("%*d ", gw-1, row+1)
		if row == e.row {
			b.WriteString(e.styles.GutterCur.Render(num))
		} else {
			b.WriteString(e.styles.Gutter.Render(num))
		}
		b.WriteString(e.renderLine(row, kinds[row], tw))
		b.WriteByte('\n')
	}
	b.WriteString(e.statusLine())
	return b.String()
}

// renderLine expands tabs, applies colours and draws the cursor.
func (e *Editor) renderLine(row int, kinds []tokKind, tw int) string {
	line := e.lines[row]
	type cell struct {
		r      rune
		k      tokKind
		cursor bool
	}
	var cells []cell
	for i, r := range line {
		cur := e.focused && row == e.row && i == e.col
		if r == '\t' {
			n := tabWidth - len(cells)%tabWidth
			for j := 0; j < n; j++ {
				cells = append(cells, cell{' ', kText, cur && j == 0})
			}
			continue
		}
		cells = append(cells, cell{r, kinds[i], cur})
	}
	if e.focused && row == e.row && e.col >= len(line) {
		cells = append(cells, cell{' ', kText, true})
	}
	if e.left < len(cells) {
		cells = cells[e.left:]
	} else {
		cells = nil
	}
	if len(cells) > tw {
		cells = cells[:tw]
	}
	var b strings.Builder
	i := 0
	for i < len(cells) {
		j := i
		for j < len(cells) && cells[j].k == cells[i].k && cells[j].cursor == cells[i].cursor {
			j++
		}
		var sb strings.Builder
		for _, c := range cells[i:j] {
			sb.WriteRune(c.r)
		}
		st := e.styles.of(cells[i].k)
		if cells[i].cursor {
			if e.mode == Insert {
				st = e.styles.CursorInsert
			} else {
				st = st.Inherit(e.styles.Cursor).Reverse(true)
			}
		}
		b.WriteString(st.Render(sb.String()))
		i = j
	}
	if len(cells) < tw {
		b.WriteString(strings.Repeat(" ", tw-len(cells)))
	}
	return b.String()
}

func (e *Editor) statusLine() string {
	var left string
	switch e.mode {
	case Insert:
		left = e.styles.StatusInsert.Render("INSERT")
	case Command:
		left = e.styles.StatusMode.Render("CMD") + " :" + string(e.cmd) + e.styles.Cursor.Render(" ")
	default:
		left = e.styles.StatusMode.Render("NORMAL")
		if e.pending != "" || e.count > 0 {
			c := ""
			if e.count > 0 {
				c = fmt.Sprint(e.count)
			}
			left += " " + c + e.pending
		}
	}
	if e.Msg != "" && e.mode != Command {
		left += " " + e.styles.Msg.Render(e.Msg)
	}
	mod := ""
	if e.dirty {
		mod = "[+] "
	}
	right := e.styles.Status.Render(fmt.Sprintf("%s%d:%d ", mod, e.row+1, e.dispCol()+1))
	gap := e.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
