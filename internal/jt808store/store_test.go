package jt808store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spool.db")
	queue, err := spool.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	store, err := Open(queue.DB())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRegisterPersistsAndReturnsAuthCode(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	authCode, err := store.Register(ctx, "013800100034", "SZRXT", "RXTmt2503d32m", now)
	if err != nil {
		t.Fatal(err)
	}
	if authCode == "" {
		t.Fatal("expected a non-empty auth code")
	}

	record, ok, err := store.Lookup(ctx, "013800100034")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected terminal to be found")
	}
	if record.AuthCode != authCode {
		t.Fatalf("Lookup AuthCode = %q, want %q", record.AuthCode, authCode)
	}
	if record.Manufacturer != "SZRXT" || record.Model != "RXTmt2503d32m" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first, err := store.Register(ctx, "013800100034", "SZRXT", "modelA", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Register(ctx, "013800100034", "SZRXT", "modelB", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("re-registration changed the auth code: %q -> %q", first, second)
	}
	record, _, err := store.Lookup(ctx, "013800100034")
	if err != nil {
		t.Fatal(err)
	}
	if record.Model != "modelB" {
		t.Fatalf("expected model to be updated on re-registration, got %q", record.Model)
	}
}

func TestAuthenticateSuccessAndFailure(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	authCode, err := store.Register(ctx, "013800100034", "SZRXT", "modelA", now)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := store.Authenticate(ctx, "013800100034", authCode)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected authentication to succeed with the correct code")
	}

	ok, err = store.Authenticate(ctx, "013800100034", "wrong-code")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected authentication to fail with an incorrect code")
	}

	ok, err = store.Authenticate(ctx, "unknown-terminal", authCode)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected authentication to fail for an unregistered terminal")
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spool.db")
	ctx := context.Background()
	now := time.Now().UTC()

	queue1, err := spool.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store1, err := Open(queue1.DB())
	if err != nil {
		t.Fatal(err)
	}
	authCode, err := store1.Register(ctx, "013800100034", "SZRXT", "modelA", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue1.Close(); err != nil {
		t.Fatal(err)
	}

	queue2, err := spool.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer queue2.Close()
	store2, err := Open(queue2.DB())
	if err != nil {
		t.Fatal(err)
	}
	ok, err := store2.Authenticate(ctx, "013800100034", authCode)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected auth code to survive a store reopen (simulated restart)")
	}
}
