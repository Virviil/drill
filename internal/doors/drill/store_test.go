package drill

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Virviil/drill/internal/core"
)

func openTest(t *testing.T) (*Store, *core.Store) {
	url := os.Getenv("DRILL_TEST_DB")
	if url == "" {
		t.Skip("set DRILL_TEST_DB to a throwaway Postgres database")
	}
	ctx := context.Background()
	cs, err := core.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Migrate(ctx, cs.DB(), "drill", Migrations()); err != nil {
		t.Fatal(err)
	}
	cs.DB().Exec(ctx, `DELETE FROM core.users WHERE nick LIKE 't-drill-%'`)
	t.Cleanup(func() {
		cs.DB().Exec(ctx, `DELETE FROM core.users WHERE nick LIKE 't-drill-%'`)
		cs.Close()
	})
	return NewStore(cs.DB()), cs
}

func TestProgress(t *testing.T) {
	s, cs := openTest(t)
	ctx := context.Background()
	u, _ := cs.CreateUser(ctx, "t-drill-p", "SHA256:drill-p", "k")

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
