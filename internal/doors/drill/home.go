package drill

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Virviil/drill/internal/catalog"
	"github.com/Virviil/drill/internal/hub"
)

type homeModel struct {
	cur int
}

type menuItem struct {
	key, title, hint string
}

var homeMenu = []menuItem{
	{"t", "Задачи", "LeetCode-задачи с проверкой"},
	{"d", "Задача дня", "следующая нерешённая из плана"},
	{"p", "План", "полугодовой план, отмечай сделанное"},
	{"s", "Статистика", "стрик, решённые, последние посылки"},
	{"q", "В хаб", "назад в главное меню"},
}

func (a *App) updateHome(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	m := &a.home
	m.cur = moveCursor(msg, m.cur, len(homeMenu))
	choice := ""
	if k.String() == "enter" || k.String() == "l" {
		choice = homeMenu[m.cur].key
	}
	for _, it := range homeMenu {
		if k.String() == it.key {
			choice = it.key
		}
	}
	switch choice {
	case "t":
		a.openTasks()
	case "d":
		if t := a.taskOfDay(); t != nil {
			return a.openTask(t)
		}
		a.err = "в этом месяце всё решено, загляни в «Задачи»"
	case "p":
		a.openPlan()
	case "s":
		return a.openStats()
	case "q":
		return hub.Back
	}
	return nil
}

// taskOfDay is the first unsolved task of the current plan month,
// falling back to the first unsolved task overall.
func (a *App) taskOfDay() *catalog.Task {
	month := 0
	if cm := a.deps.Catalog.Plan.Current(a.deps.Now()); cm != nil {
		month = cm.N
	}
	var fallback *catalog.Task
	for _, t := range a.deps.Catalog.Tasks {
		if len(a.solved[t.Slug]) > 0 {
			continue
		}
		if t.Month == month {
			return t
		}
		if fallback == nil {
			fallback = t
		}
	}
	return fallback
}

func (a *App) viewHome() string {
	var left strings.Builder
	left.WriteString(a.st.Title.Render("Тренажёр") + "\n" + a.st.Dim.Render("решай без ИИ, отмечай план") + "\n\n")
	for i, it := range homeMenu {
		line := fmt.Sprintf(" %s  %-12s", a.st.Key.Render("["+it.key+"]"), it.title)
		if i == a.home.cur {
			line = a.st.Selected.Render(fmt.Sprintf(" [%s]  %-12s", it.key, it.title))
			if it.hint != "" {
				line += " " + a.st.Dim.Render(it.hint)
			}
		}
		left.WriteString(line + "\n")
	}

	var right strings.Builder
	now := a.deps.Now()
	if cm := a.deps.Catalog.Plan.Current(now); cm != nil {
		right.WriteString(a.st.Accent.Render(cm.Title) + "\n")
		right.WriteString(a.st.Dim.Render(cm.Topics) + "\n\n")
		solvedMonth, totalMonth := 0, 0
		for _, t := range a.deps.Catalog.Tasks {
			if t.Month == cm.N {
				totalMonth++
				if len(a.solved[t.Slug]) > 0 {
					solvedMonth++
				}
			}
		}
		doneItems := 0
		for _, it := range cm.Items {
			if _, ok := a.done[it.ID]; ok {
				doneItems++
			}
		}
		right.WriteString(fmt.Sprintf("Задачи месяца   %s %d/%d\n", bar(solvedMonth, totalMonth, 16), solvedMonth, totalMonth))
		right.WriteString(fmt.Sprintf("Пункты плана    %s %d/%d\n", bar(doneItems, len(cm.Items), 16), doneItems, len(cm.Items)))
		if cm.Milestone != "" {
			right.WriteString("\n" + a.st.Dim.Render("Контрольная точка:") + "\n" + wrap(cm.Milestone, 44) + "\n")
		}
	}
	right.WriteString("\n")
	streak := fmt.Sprintf("Стрик: %d дн. (лучший %d)", a.streak.Current, a.streak.Best)
	if a.streak.Current > 0 && !a.streak.Today {
		streak += a.st.Warn.Render("  сегодня ещё ничего")
	}
	right.WriteString(streak + "\n")
	right.WriteString(fmt.Sprintf("Решено задач: %d\n", len(a.solved)))
	if t := a.taskOfDay(); t != nil {
		right.WriteString("\n" + a.st.Dim.Render("Задача дня: ") + t.Title + " " + a.st.Diff(t.Difficulty) + "\n")
	}

	l := left.String()
	r := a.st.Box.Render(strings.TrimRight(right.String(), "\n"))
	if a.w >= lipgloss.Width(l)+lipgloss.Width(r)+4 {
		return lipgloss.JoinHorizontal(lipgloss.Top, l, "   ", r)
	}
	return l + "\n" + r
}
