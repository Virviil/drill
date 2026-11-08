// Package editor is a small modal (vim-like) code editor for bubbletea.
//
// It is not a tea.Model on its own: the owner forwards key messages to
// Update and acts on the returned Action (save, run, submit, quit).
package editor

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Mode is the editor mode.
type Mode int

const (
	Normal Mode = iota
	Insert
	Command
)

func (m Mode) String() string {
	switch m {
	case Insert:
		return "INSERT"
	case Command:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// Action tells the owner what the user asked for.
type Action int

const (
	None Action = iota
	Save
	Run
	Submit
	Quit
	Reset // :reset, restore the stub
	Help
)

type snapshot struct {
	lines    [][]rune
	row, col int
}

// Editor holds the buffer, cursor and modal state.
type Editor struct {
	lines    [][]rune
	row, col int
	wantCol  int // sticky display column for j/k
	mode     Mode
	pending  string // operator prefix: "d", "y", "g", "c"
	pendN    int    // count typed before the operator
	count    int    // numeric prefix
	cmd      []rune
	undo     []snapshot
	redo     []snapshot
	reg      []string // yanked lines (linewise) or text (charwise)
	regLines bool
	top      int // first visible line
	left     int // first visible display column
	width    int
	height   int
	indent   string
	lang     string
	dirty    bool
	focused  bool

	// Message shown in the status line until the next key.
	Msg string

	hl     *highlighter
	styles Styles
}

// New creates an editor for lang ("go", "python", "rust") with text.
func New(lang, text string, styles Styles) *Editor {
	e := &Editor{lang: lang, styles: styles, focused: true}
	e.indent = "    "
	if lang == "go" {
		e.indent = "\t"
	}
	e.hl = newHighlighter(lang)
	e.SetText(text)
	return e
}

// SetText replaces the buffer and clears history.
func (e *Editor) SetText(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	parts := strings.Split(text, "\n")
	e.lines = make([][]rune, len(parts))
	for i, p := range parts {
		e.lines[i] = []rune(p)
	}
	e.row, e.col, e.top, e.left = 0, 0, 0, 0
	e.undo, e.redo = nil, nil
	e.mode = Normal
	e.dirty = false
}

// Text returns the buffer with a trailing newline.
func (e *Editor) Text() string {
	var b strings.Builder
	for _, l := range e.lines {
		b.WriteString(string(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// Dirty reports unsaved changes; MarkSaved clears it.
func (e *Editor) Dirty() bool { return e.dirty }
func (e *Editor) MarkSaved()  { e.dirty = false }

// Mode returns the current mode.
func (e *Editor) Mode() Mode { return e.mode }

// Cursor returns the 0-based row and column.
func (e *Editor) Cursor() (int, int) { return e.row, e.col }

// SetSize sets the area the editor draws into, status line included.
func (e *Editor) SetSize(w, h int) {
	e.width, e.height = w, h
	e.scroll()
}

// Focus controls whether the cursor is drawn.
func (e *Editor) Focus(on bool) { e.focused = on }

// Update handles one key.
func (e *Editor) Update(msg tea.KeyMsg) Action {
	// Over SSH "esc" followed quickly by a key arrives as alt+key.
	// In insert and command mode that means: leave the mode, then the key.
	if msg.Alt && msg.Type == tea.KeyRunes && e.mode != Normal {
		e.Update(tea.KeyMsg{Type: tea.KeyEsc})
		msg.Alt = false
	}
	// Fast typing over SSH can deliver several runes in one message;
	// outside insert mode each of them is a separate command.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && e.mode != Insert {
		var act Action
		for i, r := range msg.Runes {
			if e.mode == Insert {
				rest := tea.KeyMsg{Type: tea.KeyRunes, Runes: msg.Runes[i:]}
				if a := e.Update(rest); a != None {
					act = a
				}
				break
			}
			if a := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt}); a != None {
				act = a
			}
		}
		return act
	}
	e.Msg = ""
	var act Action
	switch e.mode {
	case Insert:
		e.insertKey(msg)
	case Command:
		act = e.commandKey(msg)
	default:
		act = e.normalKey(msg)
	}
	e.clamp()
	e.scroll()
	return act
}

// ---- normal mode ----

func (e *Editor) normalKey(msg tea.KeyMsg) Action {
	k := msg.String()
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && !msg.Alt {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' || r == '0' && e.count > 0 {
			e.count = e.count*10 + int(r-'0')
			return None
		}
	}
	n, hadCount := e.count, e.count > 0
	if n == 0 {
		n = 1
	}
	e.count = 0

	if e.pending != "" {
		p := e.pending
		e.pending = ""
		if e.pendN > 1 {
			n *= e.pendN
		}
		e.pendN = 0
		return e.operator(p, k, n)
	}

	switch k {
	case "h", "left", "backspace":
		e.col = max(0, e.col-n)
		e.wantCol = e.dispCol()
	case "l", "right", " ":
		e.col = min(e.col+n, max(0, len(e.cur())-1))
		e.wantCol = e.dispCol()
	case "j", "down", "enter":
		e.moveRow(n)
	case "k", "up":
		e.moveRow(-n)
	case "ctrl+d", "pgdown":
		e.moveRow(max(1, e.textHeight()/2) * n)
	case "ctrl+u", "pgup":
		e.moveRow(-max(1, e.textHeight()/2) * n)
	case "w":
		for i := 0; i < n; i++ {
			e.wordForward()
		}
		e.wantCol = e.dispCol()
	case "b":
		for i := 0; i < n; i++ {
			e.wordBack()
		}
		e.wantCol = e.dispCol()
	case "e":
		for i := 0; i < n; i++ {
			e.wordEnd()
		}
		e.wantCol = e.dispCol()
	case "0", "home":
		e.col, e.wantCol = 0, 0
	case "^":
		e.col = firstNonBlank(e.cur())
		e.wantCol = e.dispCol()
	case "$", "end":
		e.col = max(0, len(e.cur())-1)
		e.wantCol = 1 << 30
	case "G":
		if hadCount {
			e.row = min(n, len(e.lines)) - 1
		} else {
			e.row = len(e.lines) - 1
		}
		e.col = firstNonBlank(e.cur())
	case "g", "d", "y", "c":
		e.pending = k
		e.pendN = n
	case "x", "delete":
		if len(e.cur()) > 0 {
			e.checkpoint()
			end := min(e.col+n, len(e.cur()))
			e.reg, e.regLines = []string{string(e.cur()[e.col:end])}, false
			e.setCur(cut(e.cur(), e.col, end))
		}
	case "D":
		e.checkpoint()
		e.reg, e.regLines = []string{string(e.cur()[e.col:])}, false
		e.setCur(e.cur()[:e.col])
	case "C":
		e.checkpoint()
		e.setCur(e.cur()[:e.col])
		e.enterInsert(false)
	case "J":
		if e.row < len(e.lines)-1 {
			e.checkpoint()
			left := strings.TrimRight(string(e.cur()), " \t")
			right := strings.TrimLeft(string(e.lines[e.row+1]), " \t")
			e.col = len([]rune(left))
			if right != "" && left != "" {
				left += " "
			}
			e.setCur([]rune(left + right))
			e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
		}
	case "p":
		e.paste(true)
	case "P":
		e.paste(false)
	case "i":
		e.enterInsert(true)
	case "a":
		e.enterInsert(true)
		e.col = min(e.col+1, len(e.cur()))
	case "I":
		e.enterInsert(true)
		e.col = firstNonBlank(e.cur())
	case "A":
		e.enterInsert(true)
		e.col = len(e.cur())
	case "o", "O":
		e.checkpoint()
		ind := leadingWS(e.cur())
		if k == "o" {
			if opensBlock(e.cur(), e.lang) {
				ind += e.indent
			}
			e.insertLine(e.row+1, []rune(ind))
			e.row++
		} else {
			e.insertLine(e.row, []rune(ind))
		}
		e.col = len([]rune(ind))
		e.mode = Insert
	case "u":
		e.undoOnce()
	case "ctrl+r":
		e.redoOnce()
	case ":":
		e.mode = Command
		e.cmd = e.cmd[:0]
	case "ctrl+s":
		return Save
	case "esc", "ctrl+c", "ctrl+[":
		// nothing pending: stay in normal mode
	}
	return None
}

func (e *Editor) operator(op, k string, n int) Action {
	switch op + k {
	case "gg":
		e.row = 0
		if n > 1 {
			e.row = min(n, len(e.lines)) - 1
		}
		e.col = firstNonBlank(e.cur())
	case "dd":
		e.checkpoint()
		end := min(e.row+n, len(e.lines))
		e.reg, e.regLines = linesToStrings(e.lines[e.row:end]), true
		e.lines = append(e.lines[:e.row], e.lines[end:]...)
		if len(e.lines) == 0 {
			e.lines = [][]rune{{}}
		}
		e.row = min(e.row, len(e.lines)-1)
		e.col = firstNonBlank(e.cur())
	case "yy":
		end := min(e.row+n, len(e.lines))
		e.reg, e.regLines = linesToStrings(e.lines[e.row:end]), true
		e.Msg = fmt.Sprintf("скопировано строк: %d", end-e.row)
	case "cc":
		e.checkpoint()
		ind := leadingWS(e.cur())
		e.setCur([]rune(ind))
		e.col = len([]rune(ind))
		e.mode = Insert
	case "dw", "cw", "de", "ce":
		e.checkpoint()
		start := e.col
		for i := 0; i < n; i++ {
			if k == "w" && op == "d" {
				e.wordForwardInLine()
			} else {
				e.wordEndInLine()
			}
		}
		end := e.col
		if k == "e" || op == "c" {
			end = min(end+1, len(e.cur()))
		}
		e.reg, e.regLines = []string{string(e.cur()[start:end])}, false
		e.setCur(cut(e.cur(), start, end))
		e.col = start
		if op == "c" {
			e.mode = Insert
		}
	case "d$", "c$":
		e.checkpoint()
		e.reg, e.regLines = []string{string(e.cur()[e.col:])}, false
		e.setCur(e.cur()[:e.col])
		if op == "c" {
			e.mode = Insert
		}
	}
	return None
}

func (e *Editor) enterInsert(snap bool) {
	if snap {
		e.checkpoint()
	}
	e.mode = Insert
}

// ---- insert mode ----

func (e *Editor) insertKey(msg tea.KeyMsg) {
	if msg.Paste {
		e.insertText(string(msg.Runes), false)
		return
	}
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+[":
		e.mode = Normal
		if e.col > 0 {
			e.col--
		}
		e.wantCol = e.dispCol()
	case "enter":
		e.newline()
	case "backspace", "ctrl+h":
		e.backspace()
	case "delete":
		if e.col < len(e.cur()) {
			e.setCur(cut(e.cur(), e.col, e.col+1))
		} else if e.row < len(e.lines)-1 {
			e.setCur(append(append([]rune{}, e.cur()...), e.lines[e.row+1]...))
			e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
		}
	case "tab":
		e.insertText(e.indent, false)
	case "left":
		e.col = max(0, e.col-1)
	case "right":
		e.col = min(e.col+1, len(e.cur()))
	case "up":
		e.moveRow(-1)
	case "down":
		e.moveRow(1)
	case "home":
		e.col = 0
	case "end":
		e.col = len(e.cur())
	case "ctrl+w":
		start := e.col
		for start > 0 && unicode.IsSpace(e.cur()[start-1]) {
			start--
		}
		for start > 0 && class(e.cur()[start-1]) == class(e.cur()[max(0, e.col-1)]) && !unicode.IsSpace(e.cur()[start-1]) {
			start--
		}
		e.setCur(cut(e.cur(), start, e.col))
		e.col = start
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			runes := msg.Runes
			if msg.Type == tea.KeySpace {
				runes = []rune{' '}
			}
			for _, r := range runes {
				e.typeRune(r)
			}
		}
	}
	e.wantCol = e.dispCol()
}

func (e *Editor) typeRune(r rune) {
	line := e.cur()
	// Typing a closing bracket on a blank line dedents it.
	if (r == '}' || r == ')' || r == ']') && strings.TrimSpace(string(line[:e.col])) == "" && e.col > 0 {
		ind := []rune(leadingWS(line))
		if strings.HasSuffix(string(ind), e.indent) {
			ind = ind[:len(ind)-len([]rune(e.indent))]
			e.setCur(append(ind, line[e.col:]...))
			e.col = len(ind)
		}
	}
	e.insertText(string(r), false)
}

func (e *Editor) newline() {
	line := e.cur()
	before, after := append([]rune{}, line[:e.col]...), append([]rune{}, line[e.col:]...)
	ind := leadingWS(line)
	if e.col < len([]rune(ind)) {
		ind = string(line[:e.col])
	}
	extra := ""
	if opensBlock(before, e.lang) {
		extra = e.indent
	}
	after = []rune(strings.TrimLeft(string(after), " \t"))
	e.setCur(before)
	// "{|}" → three lines with the cursor in the middle.
	if extra != "" && len(after) > 0 && isCloser(after[0]) {
		e.insertLine(e.row+1, []rune(ind+extra))
		e.insertLine(e.row+2, append([]rune(ind), after...))
		e.row++
		e.col = len([]rune(ind + extra))
		e.dirty = true
		return
	}
	e.insertLine(e.row+1, append([]rune(ind+extra), after...))
	e.row++
	e.col = len([]rune(ind + extra))
	e.dirty = true
}

func (e *Editor) backspace() {
	if e.col > 0 {
		line := e.cur()
		// In leading whitespace made of spaces, delete one indent unit.
		if e.indent != "\t" && strings.TrimSpace(string(line[:e.col])) == "" {
			n := len([]rune(e.indent))
			start := max(0, e.col-((e.col-1)%n+1))
			e.setCur(cut(line, start, e.col))
			e.col = start
			return
		}
		e.setCur(cut(line, e.col-1, e.col))
		e.col--
		return
	}
	if e.row > 0 {
		prev := e.lines[e.row-1]
		e.col = len(prev)
		e.lines[e.row-1] = append(append([]rune{}, prev...), e.cur()...)
		e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		e.row--
		e.dirty = true
	}
}

// insertText inserts s at the cursor. Newlines split lines; no autoindent,
// so pasted code keeps its own indentation.
func (e *Editor) insertText(s string, _ bool) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	line := e.cur()
	tail := append([]rune{}, line[e.col:]...)
	head := append(append([]rune{}, line[:e.col]...), []rune(parts[0])...)
	if len(parts) == 1 {
		e.setCur(append(head, tail...))
		e.col = len(head)
		return
	}
	e.setCur(head)
	for i := 1; i < len(parts); i++ {
		r := []rune(parts[i])
		if i == len(parts)-1 {
			e.col = len(r)
			r = append(r, tail...)
		}
		e.insertLine(e.row+1, r)
		e.row++
	}
	e.dirty = true
}

// ---- command mode ----

func (e *Editor) commandKey(msg tea.KeyMsg) Action {
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+[":
		e.mode = Normal
		return None
	case "backspace":
		if len(e.cmd) == 0 {
			e.mode = Normal
			return None
		}
		e.cmd = e.cmd[:len(e.cmd)-1]
		return None
	case "enter":
		e.mode = Normal
		return e.exec(strings.TrimSpace(string(e.cmd)))
	}
	if msg.Type == tea.KeyRunes {
		e.cmd = append(e.cmd, msg.Runes...)
	} else if msg.Type == tea.KeySpace {
		e.cmd = append(e.cmd, ' ')
	}
	return None
}

func (e *Editor) exec(cmd string) Action {
	switch cmd {
	case "":
		return None
	case "w":
		return Save
	case "q", "q!", "wq", "x":
		return Quit
	case "run", "r":
		return Run
	case "submit", "s":
		return Submit
	case "reset":
		return Reset
	case "help", "h":
		return Help
	}
	// :N jumps to a line
	var n int
	if _, err := fmt.Sscanf(cmd, "%d", &n); err == nil && fmt.Sprint(n) == cmd {
		e.row = max(0, min(n, len(e.lines))-1)
		e.col = firstNonBlank(e.cur())
		return None
	}
	e.Msg = "неизвестная команда: " + cmd + "  (:help)"
	return None
}

// ---- motions ----

func (e *Editor) moveRow(d int) {
	e.row = max(0, min(e.row+d, len(e.lines)-1))
	e.col = e.colForDisp(e.wantCol)
}

const (
	clsSpace = iota
	clsWord
	clsPunct
)

func class(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return clsSpace
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return clsWord
	default:
		return clsPunct
	}
}

func (e *Editor) wordForward() {
	line := e.cur()
	if e.col < len(line) {
		c := class(line[e.col])
		for e.col < len(line) && class(line[e.col]) == c && c != clsSpace {
			e.col++
		}
	}
	for {
		line = e.cur()
		for e.col < len(line) && unicode.IsSpace(line[e.col]) {
			e.col++
		}
		if e.col < len(line) || e.row == len(e.lines)-1 {
			break
		}
		e.row++
		e.col = 0
		if len(e.cur()) == 0 {
			break
		}
	}
}

func (e *Editor) wordForwardInLine() {
	line := e.cur()
	if e.col < len(line) {
		c := class(line[e.col])
		for e.col < len(line) && class(line[e.col]) == c && c != clsSpace {
			e.col++
		}
	}
	for e.col < len(line) && unicode.IsSpace(line[e.col]) {
		e.col++
	}
}

func (e *Editor) wordEndInLine() {
	line := e.cur()
	if e.col+1 >= len(line) {
		e.col = max(0, len(line)-1)
		return
	}
	e.col++
	for e.col < len(line) && unicode.IsSpace(line[e.col]) {
		e.col++
	}
	if e.col >= len(line) {
		e.col = len(line) - 1
		return
	}
	c := class(line[e.col])
	for e.col+1 < len(line) && class(line[e.col+1]) == c {
		e.col++
	}
}

func (e *Editor) wordEnd() {
	for {
		line := e.cur()
		if e.col+1 < len(line) {
			e.wordEndInLine()
			return
		}
		if e.row == len(e.lines)-1 {
			return
		}
		e.row++
		e.col = -1
		line = e.cur()
		for e.col+1 < len(line) && unicode.IsSpace(line[e.col+1]) {
			e.col++
		}
		if e.col+1 < len(line) {
			e.col++
			c := class(line[e.col])
			for e.col+1 < len(line) && class(line[e.col+1]) == c {
				e.col++
			}
			return
		}
		e.col = 0
	}
}

func (e *Editor) wordBack() {
	for {
		line := e.cur()
		for e.col > 0 && unicode.IsSpace(line[e.col-1]) {
			e.col--
		}
		if e.col > 0 {
			c := class(line[e.col-1])
			for e.col > 0 && class(line[e.col-1]) == c {
				e.col--
			}
			return
		}
		if e.row == 0 {
			return
		}
		e.row--
		e.col = len(e.cur())
		if len(e.cur()) == 0 {
			return
		}
	}
}

// ---- editing helpers ----

func (e *Editor) cur() []rune     { return e.lines[e.row] }
func (e *Editor) setCur(r []rune) { e.lines[e.row] = r; e.dirty = true }
func (e *Editor) insertLine(at int, r []rune) {
	e.lines = append(e.lines, nil)
	copy(e.lines[at+1:], e.lines[at:])
	e.lines[at] = r
	e.dirty = true
}

func (e *Editor) paste(after bool) {
	if len(e.reg) == 0 {
		return
	}
	e.checkpoint()
	if e.regLines {
		at := e.row
		if after {
			at++
		}
		for i, s := range e.reg {
			e.insertLine(at+i, []rune(s))
		}
		e.row = at
		e.col = firstNonBlank(e.cur())
		return
	}
	if after && len(e.cur()) > 0 {
		e.col++
	}
	e.insertText(strings.Join(e.reg, "\n"), false)
	e.col = max(0, e.col-1)
}

func (e *Editor) checkpoint() {
	e.undo = append(e.undo, e.snap())
	if len(e.undo) > 200 {
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *Editor) snap() snapshot {
	cp := make([][]rune, len(e.lines))
	for i, l := range e.lines {
		cp[i] = append([]rune(nil), l...)
	}
	return snapshot{lines: cp, row: e.row, col: e.col}
}

func (e *Editor) restore(s snapshot) {
	e.lines, e.row, e.col = s.lines, s.row, s.col
	e.dirty = true
}

func (e *Editor) undoOnce() {
	if len(e.undo) == 0 {
		e.Msg = "нечего отменять"
		return
	}
	e.redo = append(e.redo, e.snap())
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.restore(s)
}

func (e *Editor) redoOnce() {
	if len(e.redo) == 0 {
		e.Msg = "нечего повторять"
		return
	}
	e.undo = append(e.undo, e.snap())
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.restore(s)
}

func (e *Editor) clamp() {
	e.row = max(0, min(e.row, len(e.lines)-1))
	limit := len(e.cur())
	if e.mode != Insert && limit > 0 {
		limit--
	}
	e.col = max(0, min(e.col, limit))
}

func cut(r []rune, from, to int) []rune {
	out := make([]rune, 0, len(r)-(to-from))
	out = append(out, r[:from]...)
	return append(out, r[to:]...)
}

func leadingWS(r []rune) string {
	i := 0
	for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
		i++
	}
	return string(r[:i])
}

func firstNonBlank(r []rune) int {
	return len([]rune(leadingWS(r)))
}

func opensBlock(r []rune, lang string) bool {
	s := strings.TrimRight(string(r), " \t")
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	if last == '{' || last == '(' || last == '[' {
		return true
	}
	return lang == "python" && last == ':'
}

func isCloser(r rune) bool { return r == '}' || r == ')' || r == ']' }

func linesToStrings(ls [][]rune) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = string(l)
	}
	return out
}
