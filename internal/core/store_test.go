package core

import (
	"context"
	"os"
	"testing"
)

// OpenTest connects to DRILL_TEST_DB with a clean core schema.
func openTest(t *testing.T) *Store {
	url := os.Getenv("DRILL_TEST_DB")
	if url == "" {
		t.Skip("set DRILL_TEST_DB to a throwaway Postgres database")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec(ctx, `DELETE FROM core.users WHERE nick LIKE 't-core-%'`)
	t.Cleanup(func() {
		s.db.Exec(ctx, `DELETE FROM core.users WHERE nick LIKE 't-core-%'`)
		s.Close()
	})
	return s
}

func TestUsersAndKeys(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "t-core-dmitry", "SHA256:core-a", "ssh-ed25519 AAA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "t-core-dmitry", "SHA256:core-b", "x"); err != ErrNickTaken {
		t.Fatalf("want ErrNickTaken, got %v", err)
	}
	got, err := s.UserByFingerprint(ctx, "SHA256:core-a")
	if err != nil || got.ID != u.ID {
		t.Fatalf("lookup: %v %+v", err, got)
	}
	if _, err := s.UserByFingerprint(ctx, "SHA256:core-zzz"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	code, err := s.NewLinkCode(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := s.LinkKey(ctx, code, "SHA256:core-laptop", "ssh-ed25519 BBB")
	if err != nil || linked.ID != u.ID {
		t.Fatalf("link: %v", err)
	}
	if _, err := s.LinkKey(ctx, code, "SHA256:core-other", "x"); err == nil {
		t.Fatal("code must be single-use")
	}
	keys, _ := s.Keys(ctx, u.ID)
	if len(keys) != 2 {
		t.Fatalf("keys: %v", keys)
	}
	// migrations are idempotent
	if err := Migrate(ctx, s.db, "core", Migrations()); err != nil {
		t.Fatal(err)
	}
}
