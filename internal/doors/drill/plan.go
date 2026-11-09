package drill

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type planModel struct {
	month int // index into Plan.Months
	cur   int
}

var kindLabel = map[string]string{
	"theory": "теория", "project": "проект", "reading": "чтение", "checkpoint": "точка",
}

func (a *App) openPlan() {
	a.refresh()
	if cm := a.deps.Catalog.Plan.Current(a.deps.Now()); cm != nil && a.scr != scrPlan {
		a.plan.month = cm.N - 1
		a.plan.cur = 0
	}
	a.scr = scrPlan
}

func (a *App) updatePlan(msg tea.Msg) tea.Cmd {
	months := a.deps.Catalog.Plan.Months
	if len(months) == 0 {
		if isKey(msg, "q", "esc") {
			a.goHome()
		}
		return nil
	}
	m := &a.plan
	items := months[m.month].Items
	m.cur = moveCursor(msg, m.cur, len(items))
	switch {
	case isKey(msg, "q", "esc"):
		a.goHome()
	case isKey(msg, "l", "right", "tab"):
		m.month = (m.month + 1) % len(months)
		m.cur = 0
	case isKey(msg, "h", "left", "shift+tab"):
		m.month = (m.month - 1 + len(months)) % len(months)
		m.cur = 0
	case isKey(msg, " ", "enter", "x"):
		if len(items) == 0 {
			return nil
		}
		on, err := a.deps.Store.ToggleItem(a.ctx, a.user.ID, items[m.cur].ID)
		if err != nil {
			a.fail(err)
			return nil
		}
		a.refresh()
		if on && m.cur < len(items)-1 {
			m.cur++
		}
	}
	return nil
}

func (a *App) viewPlan() string {
	months := a.deps.Catalog.Plan.Months
	if len(months) == 0 {
		return "План пуст: проверь plan.yaml"
	}
	m := a.plan
	cm := months[m.month]
	current := a.deps.Catalog.Plan.Current(a.deps.Now())
	var b strings.Builder

	// month tabs
	var tabs []string
	for i, mo := range months {
		label := fmt.Sprintf(" %d ", mo.N)
		switch {
		case i == m.month:
			label = a.st.Selected.Render(label)
		case current != nil && mo.N == current.N:
			label = a.st.Accent.Render(label)
		default:
			label = a.st.Dim.Render(label)
		}
		tabs = append(tabs, label)
	}
	b.WriteString(strings.Join(tabs, "") + "   " + a.st.Title.Render(cm.Title) + "\n")
	b.WriteString(a.st.Dim.Render("Алгоритмы: "+cm.Topics) + "\n")
	solved, total := 0, 0
	for _, t := range a.deps.Catalog.Tasks {
		if t.Month == cm.N {
			total++
			if len(a.solved[t.Slug]) > 0 {
				solved++
			}
		}
	}
	if total > 0 {
		b.WriteString(a.st.Dim.Render(fmt.Sprintf("Задач в тренажёре на этот месяц: %d, решено %d", total, solved)) + "\n")
	}
	b.WriteString("\n")
	for i, it := range cm.Items {
		box := "[ ]"
		if _, ok := a.done[it.ID]; ok {
			box = a.st.OK.Render("[x]")
		}
		kind := fmt.Sprintf("%-7s", kindLabel[it.Kind])
		line := fmt.Sprintf("  %s %s %s", box, a.st.Dim.Render(kind), it.Title)
		if i == m.cur {
			line = a.st.Selected.Render(" ▸") + line[2:]
		}
		b.WriteString(clipANSI(line, a.w) + "\n")
	}
	if cm.Milestone != "" {
		b.WriteString("\n" + a.st.Accent.Render("Контрольная точка: ") + wrap(cm.Milestone, max(20, a.w-20)) + "\n")
	}
	return b.String()
}
