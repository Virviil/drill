package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	url := os.Getenv("DRILL_TEST_DB")
	if url == "" {
		t.Skip("set DRILL_TEST_DB to a throwaway Postgres database")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.db.Exec(ctx, `TRUNCATE users CASCADE`)
		s.Close()
	})
	s.db.Exec(ctx, `TRUNCATE users CASCADE`)
	return s
}

func TestUsersAndKeys(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "dmitry", "SHA256:a", "ssh-ed25519 AAA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "dmitry", "SHA256:b", "x"); err != ErrNickTaken {
		t.Fatalf("want ErrNickTaken, got %v", err)
	}
	got, err := s.UserByFingerprint(ctx, "SHA256:a")
	if err != nil || got.ID != u.ID {
		t.Fatalf("lookup: %v %+v", err, got)
	}
	if _, err := s.UserByFingerprint(ctx, "SHA256:zzz"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	code, err := s.NewLinkCode(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := s.LinkKey(ctx, code, "SHA256:laptop", "ssh-ed25519 BBB")
	if err != nil || linked.ID != u.ID {
		t.Fatalf("link: %v", err)
	}
	if _, err := s.LinkKey(ctx, code, "SHA256:other", "x"); err == nil {
		t.Fatal("code must be single-use")
	}
	keys, _ := s.Keys(ctx, u.ID)
	if len(keys) != 2 {
		t.Fatalf("keys: %v", keys)
	}
	// migrations are idempotent
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProgress(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "p", "SHA256:p", "k")

	if err := s.SaveDraft(ctx, u.ID, "two-sum", "go", "v1"); err != nil {
		t.Fatal(err)
	}
	s.SaveDraft(ctx, u.ID, "two-sum", "go", "v2")
	if d, _ := s.Draft(ctx, u.ID, "two-sum", "go"); d != "v2" {
		t.Fatalf("draft %q", d)
	}

	id, err := s.StartSubmission(ctx, u.ID, "two-sum", "go", "submit", "code")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishSubmission(ctx, id, "ok", 6, 6, 20, ""); err != nil {
		t.Fatal(err)
	}
	solved, _ := s.Solved(ctx, u.ID)
	if len(solved["two-sum"]) != 1 {
		t.Fatalf("solved %v", solved)
	}
	on, err := s.ToggleItem(ctx, u.ID, "m1-baseline")
	if err != nil || !on {
		t.Fatalf("toggle on: %v %v", on, err)
	}
	st, err := s.Streak(ctx, u, time.Now())
	if err != nil || st.Current != 1 || !st.Today {
		t.Fatalf("streak %+v %v", st, err)
	}
	if on, _ := s.ToggleItem(ctx, u.ID, "m1-baseline"); on {
		t.Fatal("toggle off")
	}
}

func TestComputeStreak(t *testing.T) {
	d := func(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }
	now := d("2026-11-10").Add(15 * time.Hour)
	st := computeStreak([]time.Time{d("2026-11-01"), d("2026-11-02"), d("2026-11-03"), d("2026-11-08"), d("2026-11-09")}, now)
	if st.Current != 2 || st.Best != 3 || st.Today {
		t.Fatalf("%+v", st)
	}
	st = computeStreak([]time.Time{d("2026-11-05")}, now)
	if st.Current != 0 || st.Best != 1 {
		t.Fatalf("%+v", st)
	}
}
