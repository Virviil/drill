package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Virviil/drill/internal/catalog"
	"github.com/Virviil/drill/internal/editor"
	"github.com/Virviil/drill/internal/judge"
	"github.com/Virviil/drill/internal/store"
)

type taskModel struct {
	task     *catalog.Task
	lang     catalog.Lang
	ed       *editor.Editor
	stmtTop  int
	showStmt bool // narrow screens: statement instead of editor
	help     bool

	running bool
	submit  bool
	frame   int
	result  *judge.Result
	note    string // message above the result
}

type resultMsg struct {
	subID  int64
	submit bool
	res    judge.Result
}

type tickMsg struct{}

const wideLayout = 100

func (a *App) openTask(t *catalog.Task) tea.Cmd {
	lang := catalog.Go
	if cm := a.deps.Catalog.Plan.Current(a.deps.Now()); cm != nil {
		if _, ok := t.Langs[cm.Lang]; ok {
			lang = cm.Lang
		}
	}
	if _, ok := t.Langs[lang]; !ok {
		lang = t.LangList()[0]
	}
	code, err := a.deps.Store.Draft(a.ctx, a.user.ID, t.Slug, string(lang))
	if errors.Is(err, store.ErrNotFound) {
		code, err = t.Langs[lang].Stub, nil
	}
	if err != nil {
		a.fail(err)
		return nil
	}
	ed := editor.New(string(lang), code, editor.DefaultStyles(a.r))
	// On narrow screens start with the statement: read first, then code.
	a.task = &taskModel{task: t, lang: lang, ed: ed, showStmt: a.w < wideLayout}
	a.task.resize(a)
	a.scr = scrTask
	return nil
}

func (t *taskModel) resize(a *App) {
	w, h := t.editorSize(a)
	t.ed.SetSize(w, h)
}

func (t *taskModel) panelHeight(a *App) int {
	if t.running || t.result != nil {
		return min(10, max(4, a.h/3))
	}
	return 2
}

func (t *taskModel) stmtWidth(a *App) int {
	if a.w < wideLayout {
		return a.w
	}
	return max(36, a.w*2/5)
}

func (t *taskModel) editorSize(a *App) (int, int) {
	w := a.w
	if a.w >= wideLayout {
		w = a.w - t.stmtWidth(a) - 1
	}
	return max(10, w), max(3, a.h-1-t.panelHeight(a))
}

func (a *App) updateTask(msg tea.Msg) tea.Cmd {
	t := a.task
	switch msg := msg.(type) {
	case tickMsg:
		if !t.running {
			return nil
		}
		t.frame++
		return tick()
	case resultMsg:
		return a.onResult(msg)
	case tea.KeyMsg:
		// Keys the editor does not use in normal mode.
		if t.ed.Mode() == editor.Normal {
			switch msg.String() {
			case "[":
				t.stmtTop = max(0, t.stmtTop-3)
				return nil
			case "]":
				t.stmtTop += 3
				return nil
			case "tab":
				if a.w < wideLayout {
					t.showStmt = !t.showStmt
				}
				return nil
			case "?":
				t.help = !t.help
				if t.help && a.w < wideLayout {
					t.showStmt = true
				}
				return nil
			}
		}
		if t.showStmt && a.w < wideLayout && t.ed.Mode() == editor.Normal && msg.String() != ":" {
			if msg.String() == "esc" || msg.String() == "q" {
				t.showStmt = false
				t.help = false
			}
			return nil
		}
		switch t.ed.Update(msg) {
		case editor.Save:
			a.saveDraft()
			t.ed.Msg = "сохранено"
		case editor.Run:
			return a.check(false)
		case editor.Submit:
			return a.check(true)
		case editor.Reset:
			t.ed.SetText(t.task.Langs[t.lang].Stub)
			a.saveDraft()
			t.ed.Msg = "вернул заготовку"
		case editor.Help:
			t.help = !t.help
			if a.w < wideLayout {
				t.showStmt = t.help
			}
		case editor.Quit:
			a.saveDraft()
			a.task = nil
			a.openTasks()
		}
	}
	return nil
}

func (a *App) saveDraft() {
	t := a.task
	if t == nil {
		return
	}
	if err := a.deps.Store.SaveDraft(a.ctx, a.user.ID, t.task.Slug, string(t.lang), t.ed.Text()); err != nil {
		a.fail(err)
		return
	}
	t.ed.MarkSaved()
}

func (a *App) check(submit bool) tea.Cmd {
	t := a.task
	if t.running {
		t.ed.Msg = "проверка уже идёт"
		return nil
	}
	a.saveDraft()
	code := t.ed.Text()
	ch, err := a.deps.Queue.Submit(judge.Job{UserID: a.user.ID, Task: t.task, Lang: t.lang, Code: code, Submit: submit})
	if err != nil {
		t.ed.Msg = err.Error()
		return nil
	}
	kind := "run"
	if submit {
		kind = "submit"
	}
	id, err := a.deps.Store.StartSubmission(a.ctx, a.user.ID, t.task.Slug, string(t.lang), kind, code)
	if err != nil {
		a.fail(err)
	}
	t.running, t.submit, t.result, t.note = true, submit, nil, ""
	t.resize(a)
	wait := func() tea.Msg {
		res := <-ch
		return resultMsg{subID: id, submit: submit, res: res}
	}
	return tea.Batch(wait, tick())
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (a *App) onResult(msg resultMsg) tea.Cmd {
	res := msg.res
	if msg.subID != 0 {
		if err := a.deps.Store.FinishSubmission(a.ctx, msg.subID, string(res.Status), res.Passed, res.Total, res.TimeMS, res.Output); err != nil {
			a.fail(err)
		}
	}
	a.deps.Log.Info("check", "user", a.nick(), "sub", msg.subID, "status", res.Status, "passed", res.Passed, "total", res.Total)
	t := a.task
	if t == nil { // left the screen meanwhile
		a.refresh()
		return nil
	}
	t.running = false
	t.result = &res
	if msg.submit && res.Status == judge.StatusOK {
		before := a.streak.Current
		first := len(a.solved[t.task.Slug]) == 0
		a.refresh()
		switch {
		case first && a.streak.Current > before:
			t.note = fmt.Sprintf("Задача решена! Стрик: %d дн.", a.streak.Current)
		case first:
			t.note = "Задача решена!"
		default:
			t.note = "Принято ещё раз."
		}
	} else if !msg.submit && res.Status == judge.StatusOK {
		t.note = "Открытые тесты пройдены. Теперь :submit"
	}
	t.resize(a)
	return nil
}

// ---- view ----

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (a *App) viewTask() string {
	t := a.task
	ew, eh := t.editorSize(a)
	t.ed.SetSize(ew, eh)

	top := a.st.Logo.Render(" DRILL ") + " " + a.st.Title.Render(t.task.Title) + " " + a.st.diff(t.task.Difficulty) + " " + a.st.Dim.Render(string(t.lang))
	if len(a.solved[t.task.Slug]) > 0 {
		top += " " + a.st.OK.Render("✓")
	}
	top = spread(top, a.st.Dim.Render("? — помощь "), a.w)

	var main string
	if a.w >= wideLayout {
		sw := t.stmtWidth(a)
		stmt := t.statementView(a, sw-2, eh)
		sep := a.st.Rule.Render(strings.TrimRight(strings.Repeat("│\n", eh), "\n"))
		main = lipgloss.JoinHorizontal(lipgloss.Top, padBlock(stmt, sw-1, eh), sep, t.ed.View())
	} else if t.showStmt {
		main = padBlock(t.statementView(a, a.w-1, eh), a.w, eh)
	} else {
		main = t.ed.View()
	}
	panel := padBlock(t.panelView(a), a.w, t.panelHeight(a))
	return top + "\n" + main + "\n" + panel
}

func (t *taskModel) statementView(a *App, width, height int) string {
	var lines []string
	if t.help {
		lines = strings.Split(helpText, "\n")
		for i, l := range lines {
			lines[i] = " " + l
		}
	} else {
		lines = t.statementLines(a, width-1)
	}
	t.stmtTop = max(0, min(t.stmtTop, len(lines)-height))
	end := min(len(lines), t.stmtTop+height)
	out := lines[t.stmtTop:end]
	if t.stmtTop > 0 && len(out) > 0 {
		out[0] = a.st.Dim.Render(" ↑ [")
	}
	if end < len(lines) && len(out) > 0 {
		out[len(out)-1] = a.st.Dim.Render(" ↓ ]")
	}
	return strings.Join(out, "\n")
}

func (t *taskModel) statementLines(a *App, width int) []string {
	var lines []string
	for _, para := range strings.Split(t.task.Statement, "\n") {
		if h, ok := strings.CutPrefix(para, "# "); ok {
			lines = append(lines, " "+a.st.Title.Render(h))
			continue
		}
		for _, l := range strings.Split(wrap(para, width), "\n") {
			lines = append(lines, " "+renderInlineCode(a, l))
		}
	}
	lines = append(lines, "", " "+a.st.Accent.Render("Открытые тесты"))
	for i, tc := range t.task.PublicTests() {
		args := make([]string, len(tc.Args))
		for j, raw := range tc.Args {
			args[j] = string(raw)
		}
		in := fmt.Sprintf("%d) %s → %s", i+1, strings.Join(args, ", "), string(tc.Want))
		for _, l := range strings.Split(wrapHard(in, width), "\n") {
			lines = append(lines, " "+a.st.Dim.Render(l))
		}
	}
	return lines
}

func renderInlineCode(a *App, s string) string {
	parts := strings.Split(s, "`")
	for i := 1; i < len(parts); i += 2 {
		parts[i] = a.st.Key.Render(parts[i])
	}
	return strings.Join(parts, "")
}

func (t *taskModel) panelView(a *App) string {
	rule := a.st.Rule.Render(strings.Repeat("─", a.w))
	if t.running {
		what := "открытых тестах"
		if t.submit {
			what = "всех тестах"
		}
		return rule + "\n " + a.st.Accent.Render(spinner[t.frame%len(spinner)]) + " Проверяю на " + what + "…"
	}
	if t.result == nil {
		hint := a.st.Key.Render(":run") + " открытые тесты · " + a.st.Key.Render(":submit") + " все тесты · " +
			a.st.Key.Render(":w") + " сохранить · " + a.st.Key.Render(":q") + " к списку · " + a.st.Key.Render("[ ]") + " условие"
		if a.w < wideLayout {
			hint = a.st.Key.Render("tab") + " условие · " + a.st.Key.Render(":run") + " · " + a.st.Key.Render(":submit") + " · " + a.st.Key.Render(":q")
		}
		return rule + "\n " + hint
	}
	r := t.result
	var b strings.Builder
	b.WriteString(rule + "\n")
	head := fmt.Sprintf(" %s · %d/%d тестов", r.Status, r.Passed, r.Total)
	if r.Status == judge.StatusOK || r.Status == judge.StatusWrong {
		head += fmt.Sprintf(" · %d мс", r.TimeMS)
	}
	if r.Status == judge.StatusOK {
		b.WriteString(a.st.OK.Render("✓" + head))
	} else {
		b.WriteString(a.st.Error.Render("✗" + head))
	}
	if t.note != "" {
		b.WriteString("  " + a.st.Accent.Render(t.note))
	}
	b.WriteString("\n")
	if f := r.Failure; f != nil {
		if f.Hidden {
			fmt.Fprintf(&b, " Скрытый тест №%d", f.Index)
			if f.Err != "" {
				b.WriteString(": " + f.Err)
			}
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, " Тест №%d · вход: %s\n", f.Index, clip(f.Input, a.w-20))
			if f.Err != "" {
				fmt.Fprintf(&b, " %s\n", clip(f.Err, a.w-2))
			} else {
				fmt.Fprintf(&b, " ожидалось: %s\n получено:  %s\n", clip(f.Want, a.w-14), clip(orNull(f.Got), a.w-14))
			}
		}
	}
	if out := strings.TrimSpace(r.Output); out != "" {
		label := " вывод программы (stdout/stderr):"
		if r.Status == judge.StatusCompileError {
			label = " компилятор:"
		}
		b.WriteString(a.st.Dim.Render(label) + "\n")
		for _, l := range strings.Split(out, "\n") {
			b.WriteString(" " + clip(l, a.w-2) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func orNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

func truncateANSI(s string, w int) string { return ansi.Truncate(s, w, "") }

// padBlock makes s exactly w×h cells.
func padBlock(s string, w, h int) string {
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
			lines[i] = truncateANSI(l, w)
		}
	}
	return strings.Join(lines, "\n")
}

func wrapHard(s string, w int) string {
	r := []rune(s)
	if w <= 0 || len(r) <= w {
		return s
	}
	var out []string
	for len(r) > w {
		out = append(out, string(r[:w]))
		r = r[w:]
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	if len(out) > 4 {
		out = append(out[:4], "…")
	}
	return strings.Join(out, "\n")
}

const helpText = `Редактор — как vim, только проще.

Режимы
  i a I A o O   вставка        esc  в обычный режим
  :             команда

Движение
  h j k l       ←↓↑→           w b e  по словам
  0 ^ $         начало/конец   gg G   начало/конец файла
  ctrl+d/u      полэкрана      :12    строка 12

Правка
  x D J         символ, до конца, склеить строки
  dd yy p P     удалить/скопировать строку, вставить
  dw cw cc d$   удалить/изменить слово/строку
  3dd 2yy       со счётчиком
  u  ctrl+r     отмена / повтор

Команды
  :run   открытые тесты     :submit  все тесты
  :w     сохранить          :q       к списку задач
  :reset вернуть заготовку  :help    эта справка

Условие
  [ ]  прокрутить          tab  показать (узкий экран)
  ?    справка вкл/выкл`
