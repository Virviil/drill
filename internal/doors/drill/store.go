package drill

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Virviil/drill/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store keeps drafts, submissions and plan progress in the "drill" schema.
type Store struct {
	db *pgxpool.Pool
}

// NewStore wraps the hub's pool.
func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// Draft returns saved code or ErrNotFound.
func (s *Store) Draft(ctx context.Context, uid int64, slug, lang string) (string, error) {
	var code string
	err := s.db.QueryRow(ctx, `SELECT code FROM drill.drafts WHERE user_id = $1 AND task_slug = $2 AND lang = $3`, uid, slug, lang).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return code, err
}

// SaveDraft upserts the editor contents.
func (s *Store) SaveDraft(ctx context.Context, uid int64, slug, lang, code string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO drill.drafts (user_id, task_slug, lang, code) VALUES ($1, $2, $3, $4)
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
		INSERT INTO drill.submissions (user_id, task_slug, lang, kind, code) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		uid, slug, lang, kind, code).Scan(&id)
	return id, err
}

// FinishSubmission stores the verdict.
func (s *Store) FinishSubmission(ctx context.Context, id int64, status string, passed, total, timeMS int, output string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE drill.submissions SET status = $2, passed = $3, total = $4, time_ms = $5, output = $6, finished_at = now()
		WHERE id = $1`, id, status, passed, total, timeMS, output)
	return err
}

// RecentSubmissions lists the latest checks.
func (s *Store) RecentSubmissions(ctx context.Context, uid int64, limit int) ([]Submission, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, task_slug, lang, kind, status, coalesce(passed, 0), coalesce(total, 0), coalesce(time_ms, 0), created_at
		FROM drill.submissions WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, uid, limit)
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
		SELECT DISTINCT task_slug, lang FROM drill.submissions
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
	rows, err := s.db.Query(ctx, `SELECT DISTINCT task_slug FROM drill.submissions WHERE user_id = $1`, uid)
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
	rows, err := s.db.Query(ctx, `SELECT plan_item, done_at FROM drill.activity WHERE user_id = $1`, uid)
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
	tag, err := s.db.Exec(ctx, `DELETE FROM drill.activity WHERE user_id = $1 AND plan_item = $2`, uid, item)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}
	_, err = s.db.Exec(ctx, `INSERT INTO drill.activity (user_id, plan_item) VALUES ($1, $2) ON CONFLICT DO NOTHING`, uid, item)
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
func (s *Store) Streak(ctx context.Context, u *core.User, now time.Time) (Streak, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT d FROM (
			SELECT (created_at AT TIME ZONE $2)::date AS d FROM drill.submissions
			 WHERE user_id = $1 AND kind = 'submit' AND status = 'ok'
			UNION
			SELECT (done_at AT TIME ZONE $2)::date FROM drill.activity WHERE user_id = $1
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

// Migrations returns the drill schema migrations.
func Migrations() fs.FS {
	sub, _ := fs.Sub(migrations, "migrations")
	return sub
}
