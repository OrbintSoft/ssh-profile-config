package giveup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndGivenUp(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir, TTL: time.Hour}
	if s.GivenUp("id_rsa") {
		t.Fatal("a fresh store must not report given up")
	}
	if err := s.Record("id_rsa"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !s.GivenUp("id_rsa") {
		t.Fatal("after Record the key must be given up")
	}
	info, err := os.Stat(filepath.Join(dir, "id_rsa"))
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sentinel perm = %o, want 600", perm)
	}
}

func TestGivenUpExpiresAfterTTL(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	s := Store{Dir: dir, TTL: time.Hour, Now: func() time.Time { return now }}
	if err := s.Record("id_rsa"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if s.GivenUp("id_rsa") {
		t.Fatal("an expired record must not report given up")
	}
	if _, err := os.Stat(filepath.Join(dir, "id_rsa")); !os.IsNotExist(err) {
		t.Fatalf("an expired sentinel must be removed, stat err = %v", err)
	}
}

func TestZeroTTLNeverExpires(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	s := Store{Dir: dir, TTL: 0, Now: func() time.Time { return now }}
	if err := s.Record("id_rsa"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	now = now.Add(1000 * time.Hour)
	if !s.GivenUp("id_rsa") {
		t.Fatal("with TTL<=0 a record must never expire")
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir, TTL: time.Hour}
	if err := s.Record("id_rsa"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := s.Clear("id_rsa"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if s.GivenUp("id_rsa") {
		t.Fatal("after Clear the key must not be given up")
	}
	if err := s.Clear("absent"); err != nil {
		t.Fatalf("Clear of an absent record must not error: %v", err)
	}
}

func TestEmptyDirDisables(t *testing.T) {
	s := Store{}
	if err := s.Record("id_rsa"); err != nil {
		t.Fatalf("Record on a disabled store: %v", err)
	}
	if s.GivenUp("id_rsa") {
		t.Fatal("a disabled store must never report given up")
	}
	if err := s.Clear("id_rsa"); err != nil {
		t.Fatalf("Clear on a disabled store: %v", err)
	}
}

func TestMalformedTimestampDropped(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir, TTL: time.Hour}
	p := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(p, []byte("not-a-timestamp\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if s.GivenUp("id_rsa") {
		t.Fatal("a malformed record must not report given up")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("a malformed sentinel must be removed, stat err = %v", err)
	}
}
