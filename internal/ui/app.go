// Package ui is the BBS-style terminal interface served over SSH.
package ui

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Virviil/drill/internal/catalog"
	"github.com/Virviil/drill/internal/judge"
	"github.com/Virviil/drill/internal/store"
)

// Deps are shared by every session.
type Deps struct {
	Store   *store.Store
	Catalog *catalog.Catalog
	Queue   *judge.Queue
	Log     *slog.Logger
	Now     func() time.Time
}

// Session describes the SSH connection.
type Session struct {
	Fingerprint string
	PublicKey   string // authorized_keys line
	Remote      string
}

type screen int

const (
	scrRegister screen = iota
	scrHome
	scrTasks
	scrTask
	scrPlan
	scrStats
	scrKeys
)

// App is the root model of one SSH session.
type App struct {
	deps Deps
	sess Session
	st   styles
	r    *lipgloss.Renderer
	ctx  context.Context

	user *store.User
	scr  screen
	w, h int
	err  string // shown at the bottom of any screen

	reg   registerModel
	home  homeModel
	tasks tasksModel
	task  *taskModel
	plan  planModel
	stats statsModel
	keys  keysModel

	// progress cache, refreshed when screens open
	solved    map[string][]string
	attempted map[string]bool
	done      map[string]time.Time
	streak    store.Streak
}

// New builds the model for a session. user may be nil for a new key.
func New(ctx context.Context, deps Deps, sess Session, user *store.User, r *lipgloss.Renderer) *App {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	a := &App{deps: deps, sess: sess, r: r, st: newStyles(r), ctx: ctx, user: user, w: 80, h: 24}
	if user == nil {
		a.scr = scrRegister
	} else {
		a.goHome()
	}
	return a
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
		// Several runes in one message (fast typing, scripts): handle one by one.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && a.scr != scrTask && a.scr != scrRegister {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
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
	case scrRegister:
		cmd = a.updateRegister(msg)
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
	case scrKeys:
		cmd = a.updateKeys(msg)
	}
	return a, cmd
}

func (a *App) View() string {
	var body string
	switch a.scr {
	case scrRegister:
		body = a.viewRegister()
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
	case scrKeys:
		body = a.viewKeys()
	}
	return a.frame(body)
}

// frame puts a header and footer around a screen and fits it to the window.
func (a *App) frame(body string) string {
	title := a.st.Logo.Render(" DRILL ") + " " + a.st.Dim.Render("тренажёр без ИИ")
	if a.user != nil {
		right := a.st.Dim.Render(a.user.Nick)
		if a.streak.Current > 0 {
			right += " " + a.st.Accent.Render("▲"+itoa(a.streak.Current))
		}
		title = spread(title, right, a.w)
	}
	lines := []string{title, a.st.Rule.Render(strings.Repeat("─", max(0, a.w)))}
	lines = append(lines, strings.Split(body, "\n")...)
	footerLines := 1
	if a.err != "" {
		footerLines = 2
	}
	for len(lines) < a.h-footerLines {
		lines = append(lines, "")
	}
	if len(lines) > a.h-footerLines {
		lines = lines[:max(0, a.h-footerLines)]
	}
	if a.err != "" {
		lines = append(lines, a.st.Error.Render(clip(a.err, a.w)))
	}
	lines = append(lines, a.st.Dim.Render(clip(a.help(), a.w)))
	return strings.Join(lines, "\n")
}

func (a *App) help() string {
	switch a.scr {
	case scrRegister:
		return "enter — готово · ctrl+c — выйти"
	case scrHome:
		return "j/k — выбрать · enter — открыть · q — выйти"
	case scrTasks:
		return "j/k — выбрать · enter — решать · q — назад"
	case scrPlan:
		return "j/k — пункт · space — отметить · h/l — месяц · q — назад"
	case scrStats:
		return "q — назад"
	case scrKeys:
		return "n — код для нового ключа · q — назад"
	}
	return ""
}

func (a *App) fail(err error) {
	if err == nil {
		return
	}
	a.err = err.Error()
	a.deps.Log.Error("ui", "user", a.nick(), "err", err)
}

func (a *App) nick() string {
	if a.user == nil {
		return "-"
	}
	return a.user.Nick
}

// refresh reloads the progress caches.
func (a *App) refresh() {
	if a.user == nil {
		return
	}
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

// ---- small helpers ----

func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		return string(r[:max(0, w-1)]) + "…"
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func isKey(msg tea.Msg, keys ...string) bool {
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

// moveCursor handles j/k/up/down/g/G for a list of n items.
func moveCursor(msg tea.Msg, cur, n int) int {
	switch {
	case isKey(msg, "j", "down"):
		cur++
	case isKey(msg, "k", "up"):
		cur--
	case isKey(msg, "g", "home"):
		cur = 0
	case isKey(msg, "G", "end"):
		cur = n - 1
	case isKey(msg, "pgdown", "ctrl+d"):
		cur += 10
	case isKey(msg, "pgup", "ctrl+u"):
		cur -= 10
	}
	return max(0, min(cur, n-1))
}
