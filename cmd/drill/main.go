// Command drill serves the training TUI over SSH.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	gossh "golang.org/x/crypto/ssh"

	"github.com/Virviil/drill/internal/catalog"
	"github.com/Virviil/drill/internal/judge"
	"github.com/Virviil/drill/internal/store"
	"github.com/Virviil/drill/internal/ui"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := flag.String("addr", env("DRILL_ADDR", ":2222"), "listen address")
	dbURL := flag.String("db", env("DRILL_DB", "postgres://drill:drill@localhost:5432/drill?sslmode=disable"), "Postgres URL")
	content := flag.String("content", env("DRILL_CONTENT", "."), "directory with tasks/ and plan.yaml")
	hostKey := flag.String("hostkey", env("DRILL_HOSTKEY", ".ssh/drill_ed25519"), "host key path (created if missing)")
	sandbox := flag.String("sandbox", env("DRILL_SANDBOX", "docker"), "docker | podman | local (no isolation, dev only)")
	runtime := flag.String("runtime", env("DRILL_RUNTIME", ""), "container runtime, e.g. runsc for gVisor")
	workers := flag.Int("workers", 2, "parallel checks")
	jobsDir := flag.String("jobs", env("DRILL_JOBS_DIR", ""), "directory for job files (must be the same path on the Docker host)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cat, err := catalog.Load(*content)
	if err != nil {
		log.Error("load content", "err", err)
		os.Exit(1)
	}
	log.Info("content loaded", "tasks", len(cat.Tasks), "months", len(cat.Plan.Months))

	st, err := store.Open(ctx, *dbURL)
	if err != nil {
		log.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	var sb judge.Sandbox
	switch *sandbox {
	case "local":
		log.Warn("LOCAL SANDBOX: solutions run on the host without isolation. Never expose this.")
		sb = judge.Local{}
	case "docker", "podman":
		sb = judge.Docker{Binary: *sandbox, MemoryMB: 256, CPUs: "1", Runtime: *runtime}
	default:
		log.Error("unknown sandbox", "sandbox", *sandbox)
		os.Exit(1)
	}
	judge.JobsDir = *jobsDir
	queue := judge.NewQueue(ctx, sb, *workers)
	deps := ui.Deps{Store: st, Catalog: cat, Queue: queue, Log: log}

	handler := func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		key := s.PublicKey()
		sess := ui.Session{
			Fingerprint: gossh.FingerprintSHA256(key),
			PublicKey:   strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key))),
			Remote:      s.RemoteAddr().String(),
		}
		user, err := st.UserByFingerprint(s.Context(), sess.Fingerprint)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Error("lookup user", "err", err)
			wish.Fatalln(s, "внутренняя ошибка, попробуй позже")
			return nil, nil
		}
		model := ui.New(s.Context(), deps, sess, user, bm.MakeRenderer(s))
		return model, []tea.ProgramOption{tea.WithAltScreen()}
	}

	srv, err := wish.NewServer(
		wish.WithAddress(*addr),
		wish.WithHostKeyPath(*hostKey),
		// Any key may connect: an unknown key gets the registration screen.
		wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }),
		wish.WithIdleTimeout(30*time.Minute),
		wish.WithMiddleware(
			bm.Middleware(handler),
			activeterm.Middleware(),
			logging.StructuredMiddleware(),
		),
	)
	if err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
	go func() {
		log.Info("listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Error("serve", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}
