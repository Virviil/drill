package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Virviil/drill/internal/store"
)

type keysModel struct {
	keys []store.Key
	code string
}

func (a *App) openKeys() {
	keys, err := a.deps.Store.Keys(a.ctx, a.user.ID)
	a.fail(err)
	a.keys = keysModel{keys: keys}
	a.scr = scrKeys
}

func (a *App) updateKeys(msg tea.Msg) tea.Cmd {
	switch {
	case isKey(msg, "q", "esc", "h", "left"):
		a.goHome()
	case isKey(msg, "n"):
		code, err := a.deps.Store.NewLinkCode(a.ctx, a.user.ID)
		if err != nil {
			a.fail(err)
			return nil
		}
		a.keys.code = code
	}
	return nil
}

func (a *App) viewKeys() string {
	var b strings.Builder
	b.WriteString(a.st.Title.Render("Ключи") + "\n\n")
	for _, k := range a.keys.keys {
		mark := "  "
		if k.Fingerprint == a.sess.Fingerprint {
			mark = a.st.Accent.Render("▸ ")
		}
		fmt.Fprintf(&b, "%s%s  %s\n", mark, k.Fingerprint, a.st.Dim.Render("добавлен "+k.AddedAt.Format("02.01.2006")))
	}
	b.WriteString("\nЧтобы войти с другого ключа (ноутбук, второй компьютер):\n")
	b.WriteString("1. Нажми " + a.st.Key.Render("n") + " и получи одноразовый код (живёт 10 минут).\n")
	b.WriteString("2. Подключись с нового ключа и вместо ника введи " + a.st.Key.Render("link КОД") + ".\n")
	if a.keys.code != "" {
		b.WriteString("\nКод: " + a.st.Accent.Render(a.keys.code) + "\n")
	}
	return b.String()
}
