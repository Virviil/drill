package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type tasksModel struct {
	cur, top int
}

func (a *App) openTasks() {
	a.refresh()
	a.scr = scrTasks
}

func (a *App) updateTasks(msg tea.Msg) tea.Cmd {
	tasks := a.deps.Catalog.Tasks
	m := &a.tasks
	m.cur = moveCursor(msg, m.cur, len(tasks))
	switch {
	case isKey(msg, "q", "esc", "h", "left"):
		a.goHome()
	case isKey(msg, "enter", "l", "right"):
		if len(tasks) > 0 {
			return a.openTask(tasks[m.cur])
		}
	}
	return nil
}

func (a *App) viewTasks() string {
	tasks := a.deps.Catalog.Tasks
	m := &a.tasks
	var b strings.Builder
	solved := 0
	for _, t := range tasks {
		if len(a.solved[t.Slug]) > 0 {
			solved++
		}
	}
	b.WriteString(a.st.Title.Render("Задачи") + a.st.Dim.Render(fmt.Sprintf("  решено %d из %d", solved, len(tasks))) + "\n\n")

	rows := max(1, a.h-8)
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+rows {
		m.top = m.cur - rows + 1
	}
	month := -1
	shown := 0
	for i := m.top; i < len(tasks) && shown < rows; i++ {
		t := tasks[i]
		if t.Month != month {
			month = t.Month
			name := fmt.Sprintf("месяц %d", month)
			if month >= 1 && month <= len(a.deps.Catalog.Plan.Months) {
				name = a.deps.Catalog.Plan.Months[month-1].Title
			}
			b.WriteString(a.st.Accent.Render("── "+name) + "\n")
			shown++
		}
		mark := "  "
		switch {
		case len(a.solved[t.Slug]) > 0:
			mark = a.st.OK.Render("✓ ")
		case a.attempted[t.Slug]:
			mark = a.st.Warn.Render("• ")
		}
		title := fmt.Sprintf("%-34s", clip(t.Title, 34))
		line := fmt.Sprintf(" %s%s %s  %s", mark, title, a.st.diff(t.Difficulty), a.st.Dim.Render(t.Pattern))
		if i == m.cur {
			line = a.st.Selected.Render("▸") + mark + a.st.Selected.Render(title) + " " + a.st.diff(t.Difficulty) + "  " + a.st.Dim.Render(t.Pattern)
		}
		b.WriteString(line + "\n")
		shown++
	}
	return b.String()
}
