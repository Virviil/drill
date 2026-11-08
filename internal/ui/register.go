package ui

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Virviil/drill/internal/store"
)

type registerModel struct {
	input []rune
}

var nickRe = regexp.MustCompile(`^[a-z0-9_-]{2,20}$`)

func (a *App) updateRegister(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	m := &a.reg
	switch k.String() {
	case "esc":
		return tea.Quit
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return nil
	case "enter":
		text := strings.TrimSpace(string(m.input))
		var u *store.User
		var err error
		if code, ok := strings.CutPrefix(text, "link "); ok {
			u, err = a.deps.Store.LinkKey(a.ctx, code, a.sess.Fingerprint, a.sess.PublicKey)
		} else {
			nick := strings.ToLower(text)
			if !nickRe.MatchString(nick) {
				a.err = "ник: 2–20 символов, латиница в нижнем регистре, цифры, _ и -"
				return nil
			}
			u, err = a.deps.Store.CreateUser(a.ctx, nick, a.sess.Fingerprint, a.sess.PublicKey)
		}
		if errors.Is(err, store.ErrNickTaken) {
			a.err = "этот ник уже занят; если это ты с другого ключа, введи link КОД"
			return nil
		}
		if err != nil {
			a.err = err.Error()
			return nil
		}
		a.deps.Log.Info("registered", "nick", u.Nick, "fp", a.sess.Fingerprint)
		a.user = u
		a.goHome()
		return nil
	}
	for _, r := range k.Runes {
		if unicode.IsPrint(r) && len(m.input) < 40 {
			m.input = append(m.input, r)
		}
	}
	if k.Type == tea.KeySpace {
		m.input = append(m.input, ' ')
	}
	return nil
}

func (a *App) viewRegister() string {
	var b strings.Builder
	b.WriteString(a.st.Title.Render(banner) + "\n\n")
	b.WriteString("Привет! Этот ключ здесь впервые.\n")
	b.WriteString(a.st.Dim.Render("Твой ключ: "+a.sess.Fingerprint) + "\n\n")
	b.WriteString("Придумай ник, и ключ станет твоим логином. Паролей нет.\n")
	b.WriteString(a.st.Dim.Render("Уже есть аккаунт? Получи код в разделе «Ключи» со старого ключа и введи: link КОД") + "\n\n")
	b.WriteString(a.st.Key.Render("ник › ") + string(a.reg.input) + a.st.Selected.Render(" "))
	return b.String()
}
