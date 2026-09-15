// Package jt808store durably persists JT808 terminal identity and
// authentication codes, keyed by TerminalSN, so a terminal that
// re-registers or reconnects after a server restart keeps working with the
// same code. It shares the SQLite database file already opened by
// internal/spool (via Spool.DB()) rather than opening a second connection
// against the same file.
package jt808store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type TerminalRecord struct {
	TerminalSN   string
	AuthCode     string
	Manufacturer string
	Model        string
	RegisteredAt time.Time
	UpdatedAt    time.Time
}

type Store struct {
	db *sql.DB
}

// Open prepares the jt808_terminals table against an already-open database
// handle (typically spool.Spool.DB()).
func Open(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("jt808store: db is required")
	}
	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS jt808_terminals (
		terminal_sn   TEXT PRIMARY KEY,
		auth_code     TEXT NOT NULL,
		manufacturer  TEXT,
		model         TEXT,
		registered_at INTEGER NOT NULL,
		updated_at    INTEGER NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("initialize jt808 terminal store: %w", err)
	}
	return nil
}

// Register persists a new terminal, or returns the existing auth code
// unchanged if the terminal is already registered (idempotent: a terminal
// that re-sends 0x0100 after a firmware reset must keep working with the
// code it already has).
func (s *Store) Register(ctx context.Context, terminalSN, manufacturer, model string, now time.Time) (string, error) {
	if terminalSN == "" {
		return "", errors.New("jt808store: terminalSN is required")
	}
	if record, ok, err := s.Lookup(ctx, terminalSN); err != nil {
		return "", err
	} else if ok {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE jt808_terminals SET manufacturer=?, model=?, updated_at=? WHERE terminal_sn=?`,
			manufacturer, model, now.UTC().UnixNano(), terminalSN,
		); err != nil {
			return "", fmt.Errorf("update jt808 terminal: %w", err)
		}
		return record.AuthCode, nil
	}

	authCode, err := generateAuthCode()
	if err != nil {
		return "", err
	}
	nowNanos := now.UTC().UnixNano()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO jt808_terminals(terminal_sn, auth_code, manufacturer, model, registered_at, updated_at) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(terminal_sn) DO UPDATE SET manufacturer=excluded.manufacturer, model=excluded.model, updated_at=excluded.updated_at`,
		terminalSN, authCode, manufacturer, model, nowNanos, nowNanos,
	); err != nil {
		return "", fmt.Errorf("register jt808 terminal: %w", err)
	}
	return authCode, nil
}

// Authenticate reports whether authCode matches the persisted code for
// terminalSN. An unknown terminal or a mismatched code both return false,
// nil (not an error) so callers can log/metric a plain auth failure.
func (s *Store) Authenticate(ctx context.Context, terminalSN, authCode string) (bool, error) {
	record, ok, err := s.Lookup(ctx, terminalSN)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return record.AuthCode == authCode, nil
}

// Lookup returns the persisted record for terminalSN, if any.
func (s *Store) Lookup(ctx context.Context, terminalSN string) (TerminalRecord, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT terminal_sn, auth_code, manufacturer, model, registered_at, updated_at FROM jt808_terminals WHERE terminal_sn=?`,
		terminalSN,
	)
	var record TerminalRecord
	var registeredAt, updatedAt int64
	var manufacturer, model sql.NullString
	if err := row.Scan(&record.TerminalSN, &record.AuthCode, &manufacturer, &model, &registeredAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TerminalRecord{}, false, nil
		}
		return TerminalRecord{}, false, fmt.Errorf("lookup jt808 terminal: %w", err)
	}
	record.Manufacturer = manufacturer.String
	record.Model = model.String
	record.RegisteredAt = time.Unix(0, registeredAt).UTC()
	record.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return record, true, nil
}

func generateAuthCode() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate jt808 auth code: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
