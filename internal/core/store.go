// Package core is the hub's identity layer: users and their SSH keys.
package core

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrations returns the core schema migrations.
func Migrations() fs.FS { sub, _ := fs.Sub(migrations, "migrations"); return sub }

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrNickTaken is returned when a nick is already in use.
var ErrNickTaken = errors.New("ник занят")

// Store wraps a connection pool.
type Store struct {
	db *pgxpool.Pool
}

// Open connects to Postgres and applies the core migrations.
func Open(ctx context.Context, url string) (*Store, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := Migrate(ctx, db, "core", Migrations()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate core: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.db.Close() }

// DB is the shared pool; doors keep their tables in their own schema.
func (s *Store) DB() *pgxpool.Pool { return s.db }

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
		FROM core.user_keys k JOIN core.users u ON u.id = k.user_id
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
	err = tx.QueryRow(ctx, `INSERT INTO core.users (nick) VALUES ($1) RETURNING id, tz, created_at`, nick).
		Scan(&u.ID, &u.TZ, &u.CreatedAt)
	if isUnique(err) {
		return nil, ErrNickTaken
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO core.user_keys (fingerprint, user_id, pubkey) VALUES ($1, $2, $3)`, fp, u.ID, pubkey); err != nil {
		return nil, err
	}
	return u, tx.Commit(ctx)
}

// SetTZ updates the user's time zone (IANA name).
func (s *Store) SetTZ(ctx context.Context, uid int64, tz string) error {
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("неизвестная зона %q", tz)
	}
	_, err := s.db.Exec(ctx, `UPDATE core.users SET tz = $2 WHERE id = $1`, uid, tz)
	return err
}

// Key is one registered public key.
type Key struct {
	Fingerprint string
	AddedAt     time.Time
}

// Keys lists a user's keys.
func (s *Store) Keys(ctx context.Context, uid int64) ([]Key, error) {
	rows, err := s.db.Query(ctx, `SELECT fingerprint, added_at FROM core.user_keys WHERE user_id = $1 ORDER BY added_at`, uid)
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
	_, err := s.db.Exec(ctx, `INSERT INTO core.link_codes (code, user_id, expires_at) VALUES ($1, $2, now() + interval '10 minutes')`, code, uid)
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
	err = tx.QueryRow(ctx, `DELETE FROM core.link_codes WHERE code = $1 AND expires_at > now() RETURNING user_id`,
		strings.ToUpper(strings.TrimSpace(code))).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("код не найден или истёк")
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO core.user_keys (fingerprint, user_id, pubkey) VALUES ($1, $2, $3)`, fp, uid, pubkey); err != nil {
		return nil, err
	}
	u := &User{ID: uid}
	if err := tx.QueryRow(ctx, `SELECT nick, tz, created_at FROM core.users WHERE id = $1`, uid).Scan(&u.Nick, &u.TZ, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, tx.Commit(ctx)
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// Touch records that the user was seen now.
func (s *Store) Touch(ctx context.Context, uid int64) error {
	_, err := s.db.Exec(ctx, `UPDATE core.users SET last_seen_at = now() WHERE id = $1`, uid)
	return err
}

// Recent is a user seen recently but maybe not online.
type Recent struct {
	Nick     string
	LastSeen time.Time
}

// RecentlySeen lists users seen in the last days, newest first.
func (s *Store) RecentlySeen(ctx context.Context, days, limit int) ([]Recent, error) {
	rows, err := s.db.Query(ctx, `
		SELECT nick, last_seen_at FROM core.users
		WHERE last_seen_at > now() - make_interval(days => $1)
		ORDER BY last_seen_at DESC LIMIT $2`, days, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recent, error) {
		var x Recent
		return x, r.Scan(&x.Nick, &x.LastSeen)
	})
}
