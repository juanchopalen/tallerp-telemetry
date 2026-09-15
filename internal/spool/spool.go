package spool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
	_ "modernc.org/sqlite"
)

type Item struct {
	Event    telemetry.TelemetryEvent
	Attempts int
}

type Stats struct {
	Pending          int64
	OldestPendingAge time.Duration
	DatabaseSize     int64
}

type Spool struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Spool, error) {
	if path == "" {
		return nil, errors.New("spool path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open spool: %w", err)
	}
	db.SetMaxOpenConns(1)
	spool := &Spool{db: db, path: path}
	if err := spool.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return spool, nil
}

func (s *Spool) initialize(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS telemetry_spool (
		 id INTEGER PRIMARY KEY AUTOINCREMENT,
		 event_id TEXT NOT NULL UNIQUE,
		 payload_json BLOB NOT NULL,
		 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivered')),
		 attempts INTEGER NOT NULL DEFAULT 0,
		 next_attempt_at INTEGER NOT NULL,
		 created_at INTEGER NOT NULL,
		 delivered_at INTEGER,
		 last_error TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS telemetry_spool_pending ON telemetry_spool(status, next_attempt_at, id)`,
		`CREATE INDEX IF NOT EXISTS telemetry_spool_delivered ON telemetry_spool(status, delivered_at)`,
		`CREATE TABLE IF NOT EXISTS spool_health (id INTEGER PRIMARY KEY, checked_at INTEGER NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize spool: %w", err)
		}
	}
	return nil
}

// DB exposes the underlying SQLite handle so other durable stores that share
// this same database file (e.g. internal/jt808store) can piggyback on the
// connection already opened here instead of opening a second handle against
// one file.
func (s *Spool) DB() *sql.DB { return s.db }

func (s *Spool) Store(ctx context.Context, event telemetry.TelemetryEvent) (bool, error) {
	if event.EventID == "" {
		event.SetID()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("encode event: %w", err)
	}
	now := time.Now().UTC().UnixNano()
	result, err := s.db.ExecContext(ctx, `INSERT INTO telemetry_spool(event_id,payload_json,next_attempt_at,created_at) VALUES(?,?,?,?) ON CONFLICT(event_id) DO NOTHING`, event.EventID, payload, now, now)
	if err != nil {
		return false, fmt.Errorf("persist event: %w", err)
	}
	inserted, err := result.RowsAffected()
	return inserted > 0, err
}

func (s *Spool) Pending(ctx context.Context, limit int, now time.Time) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json,attempts FROM telemetry_spool WHERE status='pending' AND next_attempt_at<=? ORDER BY id LIMIT ?`, now.UTC().UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("read pending events: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var payload []byte
		var item Item
		if err := rows.Scan(&payload, &item.Attempts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &item.Event); err != nil {
			return nil, fmt.Errorf("decode pending event: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Spool) MarkDelivered(ctx context.Context, eventIDs []string, now time.Time) error {
	return s.updateIDs(ctx, eventIDs, func(tx *sql.Tx, id string) error {
		_, err := tx.ExecContext(ctx, `UPDATE telemetry_spool SET status='delivered',delivered_at=?,last_error=NULL WHERE event_id=?`, now.UTC().UnixNano(), id)
		return err
	})
}

func (s *Spool) MarkRetry(ctx context.Context, eventIDs []string, next time.Time, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	return s.updateIDs(ctx, eventIDs, func(tx *sql.Tx, id string) error {
		_, err := tx.ExecContext(ctx, `UPDATE telemetry_spool SET attempts=attempts+1,next_attempt_at=?,last_error=? WHERE event_id=? AND status='pending'`, next.UTC().UnixNano(), message, id)
		return err
	})
}

func (s *Spool) updateIDs(ctx context.Context, ids []string, update func(*sql.Tx, string) error) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if err := update(tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Spool) Cleanup(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM telemetry_spool WHERE status='delivered' AND delivered_at<?`, before.UTC().UnixNano())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Spool) Stats(ctx context.Context, now time.Time) (Stats, error) {
	var stats Stats
	var oldest sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MIN(created_at) FROM telemetry_spool WHERE status='pending'`).Scan(&stats.Pending, &oldest)
	if err != nil {
		return stats, err
	}
	if oldest.Valid {
		stats.OldestPendingAge = now.UTC().Sub(time.Unix(0, oldest.Int64))
		if stats.OldestPendingAge < 0 {
			stats.OldestPendingAge = 0
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(s.path + suffix); err == nil {
			stats.DatabaseSize += info.Size()
		}
	}
	return stats, nil
}

func (s *Spool) Ready(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO spool_health(id,checked_at) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET checked_at=excluded.checked_at`, time.Now().UTC().UnixNano())
	return err
}

func (s *Spool) Close() error { return s.db.Close() }

func IsDiskFull(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database or disk is full") || strings.Contains(message, "no space left on device")
}
