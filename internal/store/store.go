// Package store provides an AES-256-GCM encrypted SQLite session store.
// All values written to disk are encrypted; the master key lives only in memory.
// Per BELIEFS.md §8: platform clients read from here on every call — nothing cached.
package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/types"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// ErrNotFound is returned by Get when no session exists for the requested app.
var ErrNotFound = errors.New("store: session not found")

// Store is a thread-safe, encrypted SQLite session store.
type Store struct {
	db     *sql.DB
	gcm    cipher.AEAD
	dbPath string
}

// New opens (or creates) the SQLite database at cfg.DBPath, initialises
// the schema, and wires up AES-256-GCM using the master key from cfg.
func New(cfg *config.Config) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}

	gcm, err := newGCM(cfg.StoreMasterKey)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init cipher: %w", err)
	}

	s := &Store{db: db, gcm: gcm, dbPath: cfg.DBPath}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	slog.Info("store opened", "path", cfg.DBPath)
	return s, nil
}

// Get retrieves and decrypts the session for app. Returns ErrNotFound if absent.
func (s *Store) Get(ctx context.Context, app types.Platform) (*types.AppSession, error) {
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT ciphertext FROM sessions WHERE app = ?`, string(app),
	).Scan(&ciphertext)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get %s: %w", app, err)
	}

	session, err := s.decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("store: decrypt %s: %w", app, err)
	}
	return session, nil
}

// Set encrypts and upserts a session for app.
func (s *Store) Set(ctx context.Context, app types.Platform, session *types.AppSession) error {
	ciphertext, err := s.encrypt(session)
	if err != nil {
		return fmt.Errorf("store: encrypt %s: %w", app, err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (app, ciphertext, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(app) DO UPDATE SET
			ciphertext = excluded.ciphertext,
			updated_at = excluded.updated_at
	`, string(app), ciphertext, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: set %s: %w", app, err)
	}

	slog.Info("store: session saved", "app", app)
	return nil
}

// Delete removes the session for app. No-op if not found.
func (s *Store) Delete(ctx context.Context, app types.Platform) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE app = ?`, string(app))
	if err != nil {
		return fmt.Errorf("store: delete %s: %w", app, err)
	}
	slog.Info("store: session deleted", "app", app)
	return nil
}

// List returns the app names of all stored sessions without decrypting them.
func (s *Store) List(ctx context.Context) ([]types.Platform, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT app FROM sessions ORDER BY app`)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	defer rows.Close()

	var apps []types.Platform
	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			return nil, fmt.Errorf("store: list scan: %w", err)
		}
		apps = append(apps, types.Platform(app))
	}
	return apps, rows.Err()
}

// Backup writes a consistent snapshot of the database to path using VACUUM INTO.
func (s *Store) Backup(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("store: backup to %s: %w", path, err)
	}
	slog.Info("store: backup written", "path", path)
	return nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the sessions table if it does not exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			app        TEXT PRIMARY KEY,
			ciphertext BLOB NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	return err
}

// newGCM creates an AES-256-GCM cipher from a 32-byte key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encrypt serialises session to JSON and seals it with AES-256-GCM.
// Output format: [nonce (12 bytes)][ciphertext+tag].
func (s *Store) encrypt(session *types.AppSession) ([]byte, error) {
	plaintext, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce, giving [nonce|ct|tag].
	return s.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt splits nonce from ciphertext, opens the GCM seal, and unmarshals.
func (s *Store) decrypt(data []byte) (*types.AppSession, error) {
	ns := s.gcm.NonceSize()
	if len(data) < ns {
		return nil, errors.New("store: ciphertext too short")
	}

	nonce, ct := data[:ns], data[ns:]
	plaintext, err := s.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("store: gcm open: %w", err)
	}

	var session types.AppSession
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return nil, fmt.Errorf("store: unmarshal: %w", err)
	}
	return &session, nil
}
