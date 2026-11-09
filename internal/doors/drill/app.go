// Package drill is the trainer door: LeetCode-style tasks with a judge,
// the six-month plan and progress.
package drill

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Virviil/drill/internal/catalog"
	"github.com/Virviil/drill/internal/core"
	"github.com/Virviil/drill/internal/hub"
	"github.com/Virviil/drill/internal/judge"
	"github.com/Virviil/drill/internal/tui"
)

// Deps are shared by every session.
type Deps struct {
	Store   *Store
	Catalog *catalog.Catalog
	Queue   *judge.Queue
	Now     func() time.Time
}

// Door is the trainer as a hub door.
type Door struct{ deps Deps }

// NewDoor builds the door.
func NewDoor(deps Deps) *Door {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Door{deps: deps}
}

func (d *Door) ID() string        { return "drill" }
func (d *Door) Title() string     { return "Тренажёр" }
func (d *Door) Hint() string      { return "задачи с проверкой, план, стрик" }
func (d *Door) Migrations() fs.FS { return Migrations() }

// Summary is the line on the hub's home screen.
func (d *Door) Summary(ctx context.Context, u *core.User) string {
	solved, err := d.deps.Store.Solved(ctx, u.ID)
	if err != nil {
		return ""
	}
	streak, _ := d.deps.Store.Streak(ctx, u, d.deps.Now())
	month := ""
	monthSolved, monthTotal := 0, 0
	if cm := d.deps.Catalog.Plan.Current(d.deps.Now()); cm != nil {
		month = cm.Title
		for _, t := range d.deps.Catalog.Tasks {
			if t.Month == cm.N {
				monthTotal++
				if len(solved[t.Slug]) > 0 {
					monthSolved++
				}
			}
		}
	}
	s := fmt.Sprintf("%s: задач %d/%d · стрик %d", month, monthSolved, monthTotal, streak.Current)
	if streak.Current > 0 && !streak.Today {
		s += " · сегодня ещё ничего"
	}
	return s
}

// Open starts the door for one session.
func (d *Door) Open(ctx context.Context, s *hub.Session) tea.Model {
	a := &App{deps: d.deps, sess: s, r: s.Renderer, st: tui.NewStyles(s.Renderer), ctx: ctx,
		user: s.User, w: s.Width, h: s.Height}
	a.goHome()
	return a
}

type screen int

const (
	scrHome screen = iota
	scrTasks
	scrTask
	scrPlan
	scrStats
)

// App is the door's model for one session.
type App struct {
	deps Deps
	sess *hub.Session
	st   tui.Styles
	r    *lipgloss.Renderer
	ctx  context.Context

	user *core.User
	scr  screen
	w, h int
	err  string // shown at the bottom of any screen

	home  homeModel
	tasks tasksModel
	task  *taskModel
	plan  planModel
	stats statsModel

	// progress cache, refreshed when screens open
	solved    map[string][]string
	attempted map[string]bool
	done      map[string]time.Time
	streak    Streak
}

func (a *App) Init() tea.Cmd { return nil }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		if a.task != nil {
			a.task.resize(a)
		}
		return a, nil
	case tea.KeyMsg:
		if keys, ok := tui.SplitRunes(msg); ok && a.scr != scrTask {
			var cmds []tea.Cmd
			for _, k := range keys {
				_, cmd := a.Update(k)
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		if msg.String() == "ctrl+c" && a.scr != scrTask {
			return a, tea.Quit
		}
		a.err = ""
	}
	var cmd tea.Cmd
	switch a.scr {
	case scrHome:
		cmd = a.updateHome(msg)
	case scrTasks:
		cmd = a.updateTasks(msg)
	case scrTask:
		cmd = a.updateTask(msg)
	case scrPlan:
		cmd = a.updatePlan(msg)
	case scrStats:
		cmd = a.updateStats(msg)
	}
	return a, cmd
}

func (a *App) View() string {
	var body string
	switch a.scr {
	case scrHome:
		body = a.viewHome()
	case scrTasks:
		body = a.viewTasks()
	case scrTask:
		return a.viewTask() // full screen, draws its own error line
	case scrPlan:
		body = a.viewPlan()
	case scrStats:
		body = a.viewStats()
	}
	right := a.st.Dim.Render(a.user.Nick)
	if a.streak.Current > 0 {
		right += " " + a.st.Accent.Render("▲"+itoa(a.streak.Current))
	}
	return tui.Frame(a.st, a.w, a.h, "тренажёр без ИИ", right, body, a.err, a.help())
}

func (a *App) help() string {
	switch a.scr {
	case scrHome:
		return "j/k — выбрать · enter — открыть · q — в хаб"
	case scrTasks:
		return "j/k — выбрать · enter — решать · q — назад"
	case scrPlan:
		return "j/k — пункт · space — отметить · h/l — месяц · q — назад"
	case scrStats:
		return "q — назад"
	}
	return ""
}

func (a *App) fail(err error) {
	if err == nil {
		return
	}
	a.err = err.Error()
	a.sess.Log.Error("drill", "user", a.user.Nick, "err", err)
}

func (a *App) nick() string { return a.user.Nick }

// refresh reloads the progress caches.
func (a *App) refresh() {
	ctx := a.ctx
	var err error
	if a.solved, err = a.deps.Store.Solved(ctx, a.user.ID); err != nil {
		a.fail(err)
	}
	if a.attempted, err = a.deps.Store.Attempted(ctx, a.user.ID); err != nil {
		a.fail(err)
	}
	if a.done, err = a.deps.Store.Done(ctx, a.user.ID); err != nil {
		a.fail(err)
	}
	if a.streak, err = a.deps.Store.Streak(ctx, a.user, a.deps.Now()); err != nil {
		a.fail(err)
	}
}

func (a *App) goHome() {
	a.refresh()
	a.scr = scrHome
}

// ---- local names for shared helpers ----

func spread(l, r string, w int) string       { return tui.Spread(l, r, w) }
func clip(s string, w int) string            { return tui.Clip(s, w) }
func clipANSI(s string, w int) string        { return tui.Truncate(s, w) }
func isKey(msg tea.Msg, keys ...string) bool { return tui.IsKey(msg, keys...) }
func moveCursor(msg tea.Msg, cur, n int) int { return tui.MoveCursor(msg, cur, n) }
func bar(n, total, width int) string         { return tui.Bar(n, total, width) }
func wrap(s string, width int) string        { return tui.Wrap(s, width) }
func padBlock(s string, w, h int) string     { return tui.PadBlock(s, w, h) }
func itoa(n int) string                      { return strconv.Itoa(n) }
