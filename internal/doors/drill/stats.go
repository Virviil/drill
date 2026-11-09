package drill

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Virviil/drill/internal/judge"
)

type statsModel struct {
	recent []Submission
}

func (a *App) openStats() tea.Cmd {
	a.refresh()
	recent, err := a.deps.Store.RecentSubmissions(a.ctx, a.user.ID, 12)
	a.fail(err)
	a.stats.recent = recent
	a.scr = scrStats
	return nil
}

func (a *App) updateStats(msg tea.Msg) tea.Cmd {
	if isKey(msg, "q", "esc", "h", "left", "enter") {
		a.goHome()
	}
	return nil
}

func (a *App) viewStats() string {
	var b strings.Builder
	b.WriteString(a.st.Title.Render("Статистика") + "\n\n")
	st := a.streak
	fmt.Fprintf(&b, "Стрик        %d дн. (лучший %d, всего активных дней %d)\n", st.Current, st.Best, st.ActiveDays)

	byDiff := map[string]int{}
	totalDiff := map[string]int{}
	byLang := map[string]int{}
	for _, t := range a.deps.Catalog.Tasks {
		totalDiff[t.Difficulty]++
		if langs := a.solved[t.Slug]; len(langs) > 0 {
			byDiff[t.Difficulty]++
			for _, l := range langs {
				byLang[l]++
			}
		}
	}
	fmt.Fprintf(&b, "Решено       %d из %d\n", len(a.solved), len(a.deps.Catalog.Tasks))
	for _, d := range []string{"easy", "medium", "hard"} {
		if totalDiff[d] == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s  %s %d/%d\n", a.st.Diff(d), bar(byDiff[d], totalDiff[d], 20), byDiff[d], totalDiff[d])
	}
	var langs []string
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	if len(langs) > 0 {
		b.WriteString("По языкам   ")
		for _, l := range langs {
			fmt.Fprintf(&b, " %s: %d", l, byLang[l])
		}
		b.WriteString("\n")
	}

	b.WriteString("\n" + a.st.Accent.Render("Последние проверки") + "\n")
	if len(a.stats.recent) == 0 {
		b.WriteString(a.st.Dim.Render("пока пусто") + "\n")
	}
	for _, s := range a.stats.recent {
		title := s.TaskSlug
		if t, ok := a.deps.Catalog.Get(s.TaskSlug); ok {
			title = t.Title
		}
		status := judge.Status(s.Status).String()
		if s.Status == "queued" {
			status = "в очереди"
		}
		styled := a.st.Error.Render(status)
		if s.Status == string(judge.StatusOK) {
			styled = a.st.OK.Render(status)
		}
		kind := "run   "
		if s.Kind == "submit" {
			kind = "submit"
		}
		fmt.Fprintf(&b, " %s  %s  %-30s %s %s\n",
			a.st.Dim.Render(s.CreatedAt.In(a.user.Location()).Format("02.01 15:04")),
			a.st.Dim.Render(kind), clip(title, 30), styled,
			a.st.Dim.Render(fmt.Sprintf("%d/%d", s.Passed, s.Total)))
	}
	return b.String()
}
