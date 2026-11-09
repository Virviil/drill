// Package hub is the BBS shell: SSH identity, main menu, presence and the
// "doors" (applications) a user can enter. The trainer is one such door.
package hub

import (
	"context"
	"io/fs"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Virviil/drill/internal/core"
)

// Door is an application inside the hub.
type Door interface {
	ID() string    // stable, used for its schema and presence: "drill", "board"
	Title() string // menu entry
	Hint() string  // one line under the menu entry
	// Migrations for the door's own Postgres schema, applied at start-up
	// after the core ones. May return nil.
	Migrations() fs.FS
	// Open starts the door for one session. The returned model receives every
	// message while the door is open and leaves with hub.Back().
	Open(ctx context.Context, s *Session) tea.Model
}

// Summarizer is optionally implemented by doors that show a line on the
// hub's home screen (e.g. "Ноябрь · Go: 3/10 задач, стрик 2").
type Summarizer interface {
	Summary(ctx context.Context, u *core.User) string
}

// Session is what a door knows about the connection.
type Session struct {
	User        *core.User
	Fingerprint string
	PublicKey   string
	Renderer    *lipgloss.Renderer
	Width       int
	Height      int
	Log         *slog.Logger
	Now         func() time.Time
}

// BackMsg returns control to the hub.
type BackMsg struct{}

// Back is the command a door returns to close itself.
func Back() tea.Msg { return BackMsg{} }
