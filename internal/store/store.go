// Package store keeps users, drafts, submissions and plan progress in Postgres.
package store

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrNickTaken is returned when a nick is already in use.
var ErrNickTaken = errors.New("ник занят")

// Store wraps a connection pool.
type Store struct {
	db *pgxpool.Pool
}

// Open connects and applies pending migrations.
func Open(ctx context.Context, url string) (*Store, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close releases the pool.
func (s *Store) Close() { s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialise concurrent starts.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7234001)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("%s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// User is an account.
type User struct {
	ID        int64
	Nick      string
	TZ        string
	CreatedAt time.Time
}

// Location returns the user's time zone, UTC if unknown.
func (u *User) Location() *time.Location {
	if loc, err := time.LoadLocation(u.TZ); err == nil {
		return loc
	}
	return time.UTC
}

// UserByFingerprint finds the account a key belongs to.
func (s *Store) UserByFingerprint(ctx context.Context, fp string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.nick, u.tz, u.created_at
		FROM user_keys k JOIN users u ON u.id = k.user_id
		WHERE k.fingerprint = $1`, fp).Scan(&u.ID, &u.Nick, &u.TZ, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// CreateUser registers a nick with its first key.
func (s *Store) CreateUser(ctx context.Context, nick, fp, pubkey string) (*User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	u := &User{Nick: nick}
	err = tx.QueryRow(ctx, `INSERT INTO users (nick) VALUES ($1) RETURNING id, tz, created_at`, nick).
		Scan(&u.ID, &u.TZ, &u.CreatedAt)
	if isUnique(err) {
		return nil, ErrNickTaken
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_keys (fingerprint, user_id, pubkey) VALUES ($1, $2, $3)`, fp, u.ID, pubkey); err != nil {
		return nil, err
	}
	return u, tx.Commit(ctx)
}

// SetTZ updates the user's time zone (IANA name).
func (s *Store) SetTZ(ctx context.Context, uid int64, tz string) error {
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("неизвестная зона %q", tz)
	}
	_, err := s.db.Exec(ctx, `UPDATE users SET tz = $2 WHERE id = $1`, uid, tz)
	return err
}

// Key is one registered public key.
type Key struct {
	Fingerprint string
	AddedAt     time.Time
}

// Keys lists a user's keys.
func (s *Store) Keys(ctx context.Context, uid int64) ([]Key, error) {
	rows, err := s.db.Query(ctx, `SELECT fingerprint, added_at FROM user_keys WHERE user_id = $1 ORDER BY added_at`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Key, error) {
		var k Key
		return k, r.Scan(&k.Fingerprint, &k.AddedAt)
	})
}

// NewLinkCode creates a one-time code valid for 10 minutes.
func (s *Store) NewLinkCode(ctx context.Context, uid int64) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	code := string(b)
	_, err := s.db.Exec(ctx, `INSERT INTO link_codes (code, user_id, expires_at) VALUES ($1, $2, now() + interval '10 minutes')`, code, uid)
	return code, err
}

// LinkKey attaches a key to the account that issued code.
func (s *Store) LinkKey(ctx context.Context, code, fp, pubkey string) (*User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var uid int64
	err = tx.QueryRow(ctx, `DELETE FROM link_codes WHERE code = $1 AND expires_at > now() RETURNING user_id`,
		strings.ToUpper(strings.TrimSpace(code))).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("код не найден или истёк")
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_keys (fingerprint, user_id, pubkey) VALUES ($1, $2, $3)`, fp, uid, pubkey); err != nil {
		return nil, err
	}
	u := &User{ID: uid}
	if err := tx.QueryRow(ctx, `SELECT nick, tz, created_at FROM users WHERE id = $1`, uid).Scan(&u.Nick, &u.TZ, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, tx.Commit(ctx)
}

// Draft returns saved code or ErrNotFound.
func (s *Store) Draft(ctx context.Context, uid int64, slug, lang string) (string, error) {
	var code string
	err := s.db.QueryRow(ctx, `SELECT code FROM drafts WHERE user_id = $1 AND task_slug = $2 AND lang = $3`, uid, slug, lang).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return code, err
}

// SaveDraft upserts the editor contents.
func (s *Store) SaveDraft(ctx context.Context, uid int64, slug, lang, code string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO drafts (user_id, task_slug, lang, code) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, task_slug, lang) DO UPDATE SET code = EXCLUDED.code, updated_at = now()`,
		uid, slug, lang, code)
	return err
}

// Submission is one check of code.
type Submission struct {
	ID        int64
	TaskSlug  string
	Lang      string
	Kind      string
	Status    string
	Passed    int
	Total     int
	TimeMS    int
	CreatedAt time.Time
}

// StartSubmission records a queued check and returns its id.
func (s *Store) StartSubmission(ctx context.Context, uid int64, slug, lang, kind, code string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO submissions (user_id, task_slug, lang, kind, code) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		uid, slug, lang, kind, code).Scan(&id)
	return id, err
}

// FinishSubmission stores the verdict.
func (s *Store) FinishSubmission(ctx context.Context, id int64, status string, passed, total, timeMS int, output string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE submissions SET status = $2, passed = $3, total = $4, time_ms = $5, output = $6, finished_at = now()
		WHERE id = $1`, id, status, passed, total, timeMS, output)
	return err
}

// RecentSubmissions lists the latest checks.
func (s *Store) RecentSubmissions(ctx context.Context, uid int64, limit int) ([]Submission, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, task_slug, lang, kind, status, coalesce(passed, 0), coalesce(total, 0), coalesce(time_ms, 0), created_at
		FROM submissions WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, uid, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) {
		var x Submission
		return x, r.Scan(&x.ID, &x.TaskSlug, &x.Lang, &x.Kind, &x.Status, &x.Passed, &x.Total, &x.TimeMS, &x.CreatedAt)
	})
}

// Solved returns, per task slug, the languages it was accepted in.
func (s *Store) Solved(ctx context.Context, uid int64) (map[string][]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT task_slug, lang FROM submissions
		WHERE user_id = $1 AND kind = 'submit' AND status = 'ok' ORDER BY task_slug, lang`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var slug, lang string
		if err := rows.Scan(&slug, &lang); err != nil {
			return nil, err
		}
		out[slug] = append(out[slug], lang)
	}
	return out, rows.Err()
}

// Attempted returns task slugs with at least one submit (any status).
func (s *Store) Attempted(ctx context.Context, uid int64) (map[string]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT task_slug FROM submissions WHERE user_id = $1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out[slug] = true
	}
	return out, rows.Err()
}

// Done returns plan items the user has ticked.
func (s *Store) Done(ctx context.Context, uid int64) (map[string]time.Time, error) {
	rows, err := s.db.Query(ctx, `SELECT plan_item, done_at FROM activity WHERE user_id = $1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

// ToggleItem ticks or unticks a plan item and reports the new state.
func (s *Store) ToggleItem(ctx context.Context, uid int64, item string) (bool, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM activity WHERE user_id = $1 AND plan_item = $2`, uid, item)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}
	_, err = s.db.Exec(ctx, `INSERT INTO activity (user_id, plan_item) VALUES ($1, $2) ON CONFLICT DO NOTHING`, uid, item)
	return err == nil, err
}

// Streak is the run of consecutive active days.
type Streak struct {
	Current    int
	Best       int
	ActiveDays int
	Today      bool // something already done today
}

// Streak counts days (in the user's zone) with an accepted submit or a ticked plan item.
func (s *Store) Streak(ctx context.Context, u *User, now time.Time) (Streak, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT d FROM (
			SELECT (created_at AT TIME ZONE $2)::date AS d FROM submissions
			 WHERE user_id = $1 AND kind = 'submit' AND status = 'ok'
			UNION
			SELECT (done_at AT TIME ZONE $2)::date FROM activity WHERE user_id = $1
		) x ORDER BY d`, u.ID, u.TZ)
	if err != nil {
		return Streak{}, err
	}
	days, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		return Streak{}, err
	}
	return computeStreak(days, now.In(u.Location())), nil
}

func computeStreak(days []time.Time, now time.Time) Streak {
	var st Streak
	st.ActiveDays = len(days)
	run := 0
	var prev time.Time
	for i, d := range days {
		if i > 0 && d.Sub(prev) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > st.Best {
			st.Best = run
		}
		prev = d
	}
	if len(days) == 0 {
		return st
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	last := days[len(days)-1]
	switch today.Sub(last) {
	case 0:
		st.Today = true
		st.Current = run
	case 24 * time.Hour: // yesterday counts until the day ends
		st.Current = run
	}
	return st
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
