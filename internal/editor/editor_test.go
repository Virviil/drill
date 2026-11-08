package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// keys feeds a vim-ish key string: <esc>, <cr>, <bs>, <c-r>, <tab> are
// special, everything else is typed rune by rune.
func keys(e *Editor, s string) Action {
	var act Action
	for s != "" {
		var msg tea.KeyMsg
		switch {
		case strings.HasPrefix(s, "<esc>"):
			msg, s = tea.KeyMsg{Type: tea.KeyEsc}, s[5:]
		case strings.HasPrefix(s, "<cr>"):
			msg, s = tea.KeyMsg{Type: tea.KeyEnter}, s[4:]
		case strings.HasPrefix(s, "<bs>"):
			msg, s = tea.KeyMsg{Type: tea.KeyBackspace}, s[4:]
		case strings.HasPrefix(s, "<c-r>"):
			msg, s = tea.KeyMsg{Type: tea.KeyCtrlR}, s[5:]
		case strings.HasPrefix(s, "<tab>"):
			msg, s = tea.KeyMsg{Type: tea.KeyTab}, s[5:]
		default:
			r := []rune(s)[0]
			s = s[len(string(r)):]
			if r == ' ' {
				msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
			} else {
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
			}
		}
		if a := e.Update(msg); a != None {
			act = a
		}
	}
	return act
}

func newEd(lang, text string) *Editor {
	e := New(lang, text, DefaultStyles(lipgloss.DefaultRenderer()))
	e.SetSize(80, 20)
	return e
}

func check(t *testing.T, e *Editor, want string) {
	t.Helper()
	if got := e.Text(); got != want {
		t.Fatalf("text:\n%q\nwant:\n%q", got, want)
	}
}

func TestInsertAndEscape(t *testing.T) {
	e := newEd("go", "")
	keys(e, "ihello<esc>")
	check(t, e, "hello\n")
	if r, c := e.Cursor(); r != 0 || c != 4 || e.Mode() != Normal {
		t.Fatalf("cursor %d,%d mode %v", r, c, e.Mode())
	}
	keys(e, "A world<esc>0iX<esc>")
	check(t, e, "Xhello world\n")
}

func TestGoAutoindentBraces(t *testing.T) {
	e := newEd("go", "")
	keys(e, "ifunc f() {}<esc>")
	// cursor on '}', insert newline between braces
	keys(e, "i<cr>return 1<esc>")
	check(t, e, "func f() {\n\treturn 1\n}\n")
	keys(e, "ggoif x {<cr>y()<cr>}<esc>")
	check(t, e, "func f() {\n\tif x {\n\t\ty()\n\t}\n\treturn 1\n}\n")
}

func TestPythonIndent(t *testing.T) {
	e := newEd("python", "")
	keys(e, "idef f():<cr>return 1<cr><bs>x = 2<esc>")
	check(t, e, "def f():\n    return 1\nx = 2\n")
}

func TestDeleteYankPaste(t *testing.T) {
	e := newEd("go", "a\nb\nc\nd")
	keys(e, "jdd")
	check(t, e, "a\nc\nd\n")
	keys(e, "p")
	check(t, e, "a\nc\nb\nd\n")
	keys(e, "gg2yyGp")
	check(t, e, "a\nc\nb\nd\na\nc\n")
	keys(e, "gg3dd")
	check(t, e, "d\na\nc\n")
}

func TestUndoRedo(t *testing.T) {
	e := newEd("go", "one")
	keys(e, "A two<esc>")
	keys(e, "dd")
	check(t, e, "\n")
	keys(e, "u")
	check(t, e, "one two\n")
	keys(e, "u")
	check(t, e, "one\n")
	keys(e, "<c-r>")
	check(t, e, "one two\n")
}

func TestWordMotions(t *testing.T) {
	e := newEd("go", "foo(bar, baz)\nqux")
	keys(e, "w")
	if _, c := e.Cursor(); c != 3 {
		t.Fatalf("w → %d", c)
	}
	keys(e, "ww")
	if _, c := e.Cursor(); c != 7 {
		t.Fatalf("ww → %d", c)
	}
	keys(e, "www")
	if r, c := e.Cursor(); r != 1 || c != 0 {
		t.Fatalf("to next line: %d,%d", r, c)
	}
	keys(e, "b")
	if r, c := e.Cursor(); r != 0 || c != 12 {
		t.Fatalf("b → %d,%d", r, c)
	}
	keys(e, "0dw")
	check(t, e, "(bar, baz)\nqux\n")
	keys(e, "lcwxyz<esc>")
	check(t, e, "(xyz, baz)\nqux\n")
}

func TestMisc(t *testing.T) {
	e := newEd("go", "abc\n  def")
	keys(e, "x")
	check(t, e, "bc\n  def\n")
	keys(e, "J")
	check(t, e, "bc def\n")
	keys(e, "0D")
	check(t, e, "\n")
	if keys(e, ":w<cr>") != Save || keys(e, ":run<cr>") != Run || keys(e, ":s<cr>") != Submit || keys(e, ":q<cr>") != Quit {
		t.Fatal("commands")
	}
	keys(e, ":nope<cr>")
	if !strings.Contains(e.Msg, "неизвестная") {
		t.Fatalf("msg %q", e.Msg)
	}
}

func TestPasteKeepsIndent(t *testing.T) {
	e := newEd("go", "")
	keys(e, "i")
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("func f() {\n\treturn\n}"), Paste: true})
	keys(e, "<esc>")
	check(t, e, "func f() {\n\treturn\n}\n")
}

func TestViewSize(t *testing.T) {
	e := newEd("go", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}")
	e.SetSize(40, 10)
	lines := strings.Split(e.View(), "\n")
	if len(lines) != 10 {
		t.Fatalf("view has %d lines", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 40 {
			t.Fatalf("line %d width %d: %q", i, w, l)
		}
	}
}

func TestBatchedRunes(t *testing.T) {
	e := newEd("go", "a\nb\nc")
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jddOxy")})
	keys(e, "<esc>")
	check(t, e, "a\nxy\nc\n")
	if e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":run")}) != None {
		t.Fatal("no action before enter")
	}
	if keys(e, "<cr>") != Run {
		t.Fatal("run")
	}
}

func TestAltAsEscape(t *testing.T) {
	e := newEd("go", "")
	keys(e, "iab")
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}, Alt: true})
	if e.Mode() != Command {
		t.Fatalf("mode %v", e.Mode())
	}
	if keys(e, "w<cr>") != Save {
		t.Fatal("save")
	}
	check(t, e, "ab\n")
}
