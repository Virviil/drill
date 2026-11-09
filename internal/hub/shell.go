package hub

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Virviil/drill/internal/core"
	"github.com/Virviil/drill/internal/tui"
)

// Hub is shared by all sessions.
type Hub struct {
	Core     *core.Store
	Doors    []Door
	Presence *Presence
}

type screen int

const (
	scrRegister screen = iota
	scrHome
	scrKeys
	scrWho
)

// Shell is the root model of one SSH session.
type Shell struct {
	hub  *Hub
	sess *Session
	ctx  context.Context
	st   tui.Styles

	scr      screen
	cur      int
	err      string
	door     tea.Model // non-nil while a door is open
	presence int

	input     []rune // registration
	keys      []core.Key
	linkCode  string
	recent    []core.Recent
	summaries map[string]string
}

// NewShell builds the model for a session; sess.User is nil for a new key.
func (h *Hub) NewShell(ctx context.Context, sess *Session) *Shell {
	sh := &Shell{hub: h, sess: sess, ctx: ctx, st: tui.NewStyles(sess.Renderer)}
	if sess.User == nil {
		sh.scr = scrRegister
	} else {
		sh.login()
	}
	return sh
}

// Close unregisters the session from presence.
func (sh *Shell) Close() {
	if sh.presence != 0 {
		sh.hub.Presence.Leave(sh.presence)
	}
}

func (sh *Shell) login() {
	sh.presence = sh.hub.Presence.Join(sh.sess.User.Nick)
	sh.fail(sh.hub.Core.Touch(sh.ctx, sh.sess.User.ID))
	sh.goHome()
}

func (sh *Shell) Init() tea.Cmd { return nil }

func (sh *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.WindowSizeMsg); ok {
		sh.sess.Width, sh.sess.Height = m.Width, m.Height
	}
	if _, ok := msg.(BackMsg); ok {
		sh.door = nil
		sh.hub.Presence.Move(sh.presence, "")
		sh.goHome()
		return sh, nil
	}
	if sh.door != nil {
		var cmd tea.Cmd
		sh.door, cmd = sh.door.Update(msg)
		return sh, cmd
	}
	if keys, ok := tui.SplitRunes(msg); ok && sh.scr != scrRegister {
		var cmds []tea.Cmd
		for _, k := range keys {
			_, cmd := sh.Update(k)
			cmds = append(cmds, cmd)
		}
		return sh, tea.Batch(cmds...)
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "ctrl+c" {
			return sh, tea.Quit
		}
		sh.err = ""
	}
	switch sh.scr {
	case scrRegister:
		return sh, sh.updateRegister(msg)
	case scrHome:
		return sh, sh.updateHome(msg)
	case scrKeys:
		sh.updateKeys(msg)
	case scrWho:
		if tui.IsKey(msg, "q", "esc", "h", "left", "enter") {
			sh.goHome()
		}
	}
	return sh, nil
}

func (sh *Shell) View() string {
	if sh.door != nil {
		return sh.door.View()
	}
	var body, help string
	switch sh.scr {
	case scrRegister:
		body, help = sh.viewRegister(), "enter — готово · ctrl+c — выйти"
	case scrHome:
		body, help = sh.viewHome(), "j/k — выбрать · enter — открыть · q — выйти"
	case scrKeys:
		body, help = sh.viewKeys(), "n — код для нового ключа · q — назад"
	case scrWho:
		body, help = sh.viewWho(), "q — назад"
	}
	right := ""
	if sh.sess.User != nil {
		right = sh.st.Dim.Render(fmt.Sprintf("%s · онлайн %d", sh.sess.User.Nick, len(sh.hub.Presence.List())))
	}
	return tui.Frame(sh.st, sh.sess.Width, sh.sess.Height, "хаб", right, body, sh.err, help)
}

func (sh *Shell) fail(err error) {
	if err == nil {
		return
	}
	sh.err = err.Error()
	sh.sess.Log.Error("hub", "err", err)
}

// ---- home ----

type menuItem struct {
	key, title, hint string
	door             Door
}

func (sh *Shell) menu() []menuItem {
	var items []menuItem
	keys := "123456789"
	for i, d := range sh.hub.Doors {
		items = append(items, menuItem{key: string(keys[i%len(keys)]), title: d.Title(), hint: d.Hint(), door: d})
	}
	return append(items,
		menuItem{key: "w", title: "Кто онлайн", hint: "кто сейчас здесь и кто заходил"},
		menuItem{key: "k", title: "Ключи", hint: "привязать ещё один SSH-ключ"},
		menuItem{key: "q", title: "Выход"},
	)
}

func (sh *Shell) goHome() {
	sh.scr = scrHome
	sh.summaries = map[string]string{}
	for _, d := range sh.hub.Doors {
		if s, ok := d.(Summarizer); ok {
			sh.summaries[d.ID()] = s.Summary(sh.ctx, sh.sess.User)
		}
	}
}

func (sh *Shell) updateHome(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	items := sh.menu()
	sh.cur = tui.MoveCursor(msg, sh.cur, len(items))
	var choice *menuItem
	if k.String() == "enter" || k.String() == "l" {
		choice = &items[sh.cur]
	}
	for i := range items {
		if k.String() == items[i].key {
			choice = &items[i]
		}
	}
	if choice == nil {
		return nil
	}
	switch {
	case choice.door != nil:
		return sh.open(choice.door)
	case choice.key == "w":
		recent, err := sh.hub.Core.RecentlySeen(sh.ctx, 7, 20)
		sh.fail(err)
		sh.recent = recent
		sh.scr = scrWho
	case choice.key == "k":
		keys, err := sh.hub.Core.Keys(sh.ctx, sh.sess.User.ID)
		sh.fail(err)
		sh.keys, sh.linkCode = keys, ""
		sh.scr = scrKeys
	case choice.key == "q":
		return tea.Quit
	}
	return nil
}

func (sh *Shell) open(d Door) tea.Cmd {
	sh.door = d.Open(sh.ctx, sh.sess)
	sh.hub.Presence.Move(sh.presence, d.ID())
	size := tea.WindowSizeMsg{Width: sh.sess.Width, Height: sh.sess.Height}
	return tea.Batch(sh.door.Init(), func() tea.Msg { return size })
}

func (sh *Shell) viewHome() string {
	var left strings.Builder
	if sh.sess.Height >= 34 {
		left.WriteString(sh.st.Title.Render(tui.Banner) + "\n\n")
	} else {
		left.WriteString("\n")
	}
	for i, it := range sh.menu() {
		line := fmt.Sprintf(" %s  %-14s", sh.st.Key.Render("["+it.key+"]"), it.title)
		if i == sh.cur {
			line = sh.st.Selected.Render(fmt.Sprintf(" [%s]  %-14s", it.key, it.title))
			if it.hint != "" {
				line += " " + sh.st.Dim.Render(it.hint)
			}
		}
		left.WriteString(line + "\n")
	}

	var right strings.Builder
	right.WriteString(sh.st.Accent.Render("Привет, "+sh.sess.User.Nick) + "\n\n")
	for _, d := range sh.hub.Doors {
		if s := sh.summaries[d.ID()]; s != "" {
			right.WriteString(sh.st.Title.Render(d.Title()) + "\n" + s + "\n\n")
		}
	}
	var nicks []string
	for _, o := range sh.hub.Presence.List() {
		nicks = append(nicks, o.Nick)
	}
	right.WriteString(sh.st.Dim.Render("Сейчас здесь: ") + tui.Wrap(strings.Join(nicks, ", "), 40))

	l := left.String()
	r := sh.st.Box.Render(strings.TrimRight(right.String(), "\n"))
	if sh.sess.Width >= lipgloss.Width(l)+lipgloss.Width(r)+4 {
		return lipgloss.JoinHorizontal(lipgloss.Top, l, "   ", r)
	}
	return l + "\n" + r
}

// ---- who ----

func (sh *Shell) doorTitle(id string) string {
	if id == "" {
		return "главное меню"
	}
	for _, d := range sh.hub.Doors {
		if d.ID() == id {
			return d.Title()
		}
	}
	return id
}

func (sh *Shell) viewWho() string {
	var b strings.Builder
	b.WriteString(sh.st.Title.Render("Кто онлайн") + "\n\n")
	here := map[string]bool{}
	for _, o := range sh.hub.Presence.List() {
		here[o.Nick] = true
		fmt.Fprintf(&b, " %s %-20s %s %s\n", sh.st.OK.Render("●"), o.Nick,
			sh.st.Dim.Render(fmt.Sprintf("%-16s", sh.doorTitle(o.Door))),
			sh.st.Dim.Render("с "+o.Since.In(sh.sess.User.Location()).Format("15:04")))
	}
	b.WriteString("\n" + sh.st.Accent.Render("Заходили за неделю") + "\n")
	n := 0
	for _, r := range sh.recent {
		if here[r.Nick] {
			continue
		}
		n++
		fmt.Fprintf(&b, " %s %-20s %s\n", sh.st.Dim.Render("○"), r.Nick,
			sh.st.Dim.Render(r.LastSeen.In(sh.sess.User.Location()).Format("02.01 15:04")))
	}
	if n == 0 {
		b.WriteString(sh.st.Dim.Render(" никого") + "\n")
	}
	return b.String()
}

// ---- keys ----

func (sh *Shell) updateKeys(msg tea.Msg) {
	switch {
	case tui.IsKey(msg, "q", "esc", "h", "left"):
		sh.goHome()
	case tui.IsKey(msg, "n"):
		code, err := sh.hub.Core.NewLinkCode(sh.ctx, sh.sess.User.ID)
		if err != nil {
			sh.fail(err)
			return
		}
		sh.linkCode = code
	}
}

func (sh *Shell) viewKeys() string {
	var b strings.Builder
	b.WriteString(sh.st.Title.Render("Ключи") + "\n\n")
	for _, k := range sh.keys {
		mark := "  "
		if k.Fingerprint == sh.sess.Fingerprint {
			mark = sh.st.Accent.Render("▸ ")
		}
		fmt.Fprintf(&b, "%s%s  %s\n", mark, k.Fingerprint, sh.st.Dim.Render("добавлен "+k.AddedAt.Format("02.01.2006")))
	}
	b.WriteString("\nЧтобы войти с другого ключа (ноутбук, второй компьютер):\n")
	b.WriteString("1. Нажми " + sh.st.Key.Render("n") + " и получи одноразовый код (живёт 10 минут).\n")
	b.WriteString("2. Подключись с нового ключа и вместо ника введи " + sh.st.Key.Render("link КОД") + ".\n")
	if sh.linkCode != "" {
		b.WriteString("\nКод: " + sh.st.Accent.Render(sh.linkCode) + "\n")
	}
	return b.String()
}

// ---- registration ----

var nickRe = regexp.MustCompile(`^[a-z0-9_-]{2,20}$`)

func (sh *Shell) updateRegister(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc":
		return tea.Quit
	case "backspace":
		if len(sh.input) > 0 {
			sh.input = sh.input[:len(sh.input)-1]
		}
		return nil
	case "enter":
		text := strings.TrimSpace(string(sh.input))
		var u *core.User
		var err error
		if code, ok := strings.CutPrefix(text, "link "); ok {
			u, err = sh.hub.Core.LinkKey(sh.ctx, code, sh.sess.Fingerprint, sh.sess.PublicKey)
		} else {
			nick := strings.ToLower(text)
			if !nickRe.MatchString(nick) {
				sh.err = "ник: 2–20 символов, латиница в нижнем регистре, цифры, _ и -"
				return nil
			}
			u, err = sh.hub.Core.CreateUser(sh.ctx, nick, sh.sess.Fingerprint, sh.sess.PublicKey)
		}
		if errors.Is(err, core.ErrNickTaken) {
			sh.err = "этот ник уже занят; если это ты с другого ключа, введи link КОД"
			return nil
		}
		if err != nil {
			sh.err = err.Error()
			return nil
		}
		sh.sess.Log.Info("registered", "nick", u.Nick, "fp", sh.sess.Fingerprint)
		sh.sess.User = u
		sh.login()
		return nil
	}
	for _, r := range k.Runes {
		if unicode.IsPrint(r) && len(sh.input) < 40 {
			sh.input = append(sh.input, r)
		}
	}
	if k.Type == tea.KeySpace {
		sh.input = append(sh.input, ' ')
	}
	return nil
}

func (sh *Shell) viewRegister() string {
	var b strings.Builder
	b.WriteString(sh.st.Title.Render(tui.Banner) + "\n\n")
	b.WriteString("Привет! Этот ключ здесь впервые.\n")
	b.WriteString(sh.st.Dim.Render("Твой ключ: "+sh.sess.Fingerprint) + "\n\n")
	b.WriteString("Придумай ник, и ключ станет твоим логином. Паролей нет.\n")
	b.WriteString(sh.st.Dim.Render("Уже есть аккаунт? Получи код в разделе «Ключи» со старого ключа и введи: link КОД") + "\n\n")
	b.WriteString(sh.st.Key.Render("ник › ") + string(sh.input) + sh.st.Selected.Render(" "))
	return b.String()
}
