// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.db.Exec(`INSERT INTO users(username, display_name, password_hash, role, created_at)
		VALUES('admin', 'Admin', 'hash', 'admin', ?)`, time.Now().Unix()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return s
}

func setupOrder(t *testing.T, s *Store) int64 {
	t.Helper()
	id, err := s.CreateOrder("Testauftrag", "Beschreibung", 0, 1)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := s.AddTask(id, "Konzeption", 60); err != nil {
		t.Fatalf("add task 1: %v", err)
	}
	if err := s.AddTask(id, "Umsetzung", 120); err != nil {
		t.Fatalf("add task 2: %v", err)
	}
	return id
}

func TestOrderSollMinutes(t *testing.T) {
	s := testStore(t)

	// Without an explicit Soll the sum of the task minutes is used, so
	// existing orders keep their previous behaviour.
	fallbackID := setupOrder(t, s)
	o, err := s.Order(fallbackID)
	if err != nil {
		t.Fatal(err)
	}
	if o.SollMinutes != 0 || o.PlannedMinutes != 180 {
		t.Fatalf("fallback Soll: explicit=%d effective=%d", o.SollMinutes, o.PlannedMinutes)
	}

	explicitID, err := s.CreateOrder("Soll-Auftrag", "", 90, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTask(explicitID, "Teil", 30); err != nil {
		t.Fatal(err)
	}
	o, err = s.Order(explicitID)
	if err != nil {
		t.Fatal(err)
	}
	if o.SollMinutes != 90 || o.PlannedMinutes != 90 {
		t.Fatalf("explicit Soll: explicit=%d effective=%d", o.SollMinutes, o.PlannedMinutes)
	}
	rep, err := s.Report(explicitID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.PlannedMinutes != 90 {
		t.Fatalf("report Soll: %d", rep.PlannedMinutes)
	}

	// Resetting to 0 restores the task-based fallback.
	if err := s.SetPlannedMinutes(explicitID, 0); err != nil {
		t.Fatal(err)
	}
	o, err = s.Order(explicitID)
	if err != nil {
		t.Fatal(err)
	}
	if o.SollMinutes != 0 || o.PlannedMinutes != 30 {
		t.Fatalf("Soll after reset: explicit=%d effective=%d", o.SollMinutes, o.PlannedMinutes)
	}
}

func rewindOpenSegment(t *testing.T, s *Store, seconds int64) {
	t.Helper()
	_, err := s.db.Exec(`UPDATE time_segments SET start_at = start_at - ? WHERE end_at IS NULL`, seconds)
	if err != nil {
		t.Fatalf("rewind segment: %v", err)
	}
	_, err = s.db.Exec(`UPDATE work_orders SET timer_started_at = timer_started_at - ? WHERE timer_state = 'running'`, seconds)
	if err != nil {
		t.Fatalf("rewind order: %v", err)
	}
}

func TestTimerFlowAndReport(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)

	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	rewindOpenSegment(t, s, 600) // 10 minutes

	if err := s.TimerAction(id, 1, "pause"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	snap, err := s.TimerSnapshot(id)
	if err != nil || snap.State != "paused" || snap.Elapsed != 600 {
		t.Fatalf("snapshot after pause: %+v, %v", snap, err)
	}

	if err := s.TimerAction(id, 1, "resume"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	rewindOpenSegment(t, s, 300) // another 5 minutes

	if err := s.TimerAction(id, 1, "stop"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	snap, err = s.TimerSnapshot(id)
	if err != nil || snap.State != "stopped" || snap.Elapsed != 900 {
		t.Fatalf("snapshot after stop: %+v, %v", snap, err)
	}

	rep, err := s.Report(id)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if rep.PlannedMinutes != 180 || rep.ActualSeconds != 900 {
		t.Fatalf("unexpected report values: %+v", rep)
	}
	if rep.DeviationSeconds != 900-180*60 {
		t.Fatalf("unexpected deviation: %d", rep.DeviationSeconds)
	}
	if rep.Summary == "" || len(rep.Tasks) != 2 {
		t.Fatalf("unexpected summary/tasks: %+v", rep)
	}

	o, err := s.Order(id)
	if err != nil || !o.Done() {
		t.Fatalf("order should be done: %+v, %v", o, err)
	}
}

func TestTimerConflictAndReopen(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)

	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.TimerAction(id, 1, "start"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict on double start, got %v", err)
	}
	if err := s.TimerAction(id, 1, "stop"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := s.TimerAction(id, 1, "pause"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict on pause after stop, got %v", err)
	}
	if err := s.TimerAction(id, 1, "reopen"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	o, err := s.Order(id)
	if err != nil || o.Done() {
		t.Fatalf("order should be active again: %+v, %v", o, err)
	}
	if _, err := s.Report(id); err != nil {
		t.Fatalf("report after reopen: %v", err)
	}
}

func TestOrderShowsRunningTime(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)
	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	rewindOpenSegment(t, s, 120)

	o, err := s.Order(id)
	if err != nil {
		t.Fatal(err)
	}
	if o.ActualSeconds < 120 {
		t.Fatalf("running segment missing from order total: %d", o.ActualSeconds)
	}
	if o.DeltaSeconds() != o.ActualSeconds-o.PlannedMinutes*60 {
		t.Fatal("delta does not match live values")
	}
}

func TestRecoverTimers(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)

	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	rewindOpenSegment(t, s, 120)

	count, err := s.RecoverTimers()
	if err != nil || count != 1 {
		t.Fatalf("recover: count=%d err=%v", count, err)
	}
	snap, err := s.TimerSnapshot(id)
	if err != nil || snap.State != "paused" || snap.Elapsed < 120 {
		t.Fatalf("snapshot after recovery: %+v, %v", snap, err)
	}

	o, err := s.Order(id)
	if err != nil || !o.Interrupted {
		t.Fatalf("order should be flagged as interrupted: %+v, %v", o, err)
	}
}

func TestTouchRunningTimers(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)
	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	past := time.Now().Add(-time.Hour).Unix()
	if _, err := s.db.Exec(`UPDATE work_orders SET timer_last_seen = ?`, past); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchRunningTimers(); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	var lastSeen int64
	if err := s.db.QueryRow(`SELECT timer_last_seen FROM work_orders WHERE id = ?`, id).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen <= past {
		t.Fatalf("heartbeat did not update: %d <= %d", lastSeen, past)
	}
	if err := s.TimerAction(id, 1, "pause"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE work_orders SET timer_last_seen = ?`, past); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchRunningTimers(); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT timer_last_seen FROM work_orders WHERE id = ?`, id).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen != past {
		t.Fatalf("paused order should not get heartbeats, got %d", lastSeen)
	}
}

func TestBackup(t *testing.T) {
	s := testStore(t)
	setupOrder(t, s)
	dest := t.TempDir() + "/snapshot.db"
	if err := s.Backup(dest); err != nil {
		t.Fatalf("backup: %v", err)
	}
	other, err := Open(dest)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer other.Close()
	orders, err := other.Orders()
	if err != nil || len(orders) != 1 {
		t.Fatalf("snapshot orders: %d, %v", len(orders), err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	s := testStore(t)
	hash := "hash"
	id, err := s.CreateUser("mia", "Mia", hash, "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := s.Users()
	if err != nil || len(users) != 2 {
		t.Fatalf("users: %d, %v", len(users), err)
	}
	if err := s.CreateSession(id, "tokenhash", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("create session: %v", err)
	}
	u, err := s.SessionUser("tokenhash")
	if err != nil || u.ID != id {
		t.Fatalf("session user: %+v, %v", u, err)
	}
	if err := s.SetUserActive(id, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := s.SessionUser("tokenhash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive user should not log in, got %v", err)
	}
}

func TestLastActiveAdminIsProtected(t *testing.T) {
	s := testStore(t)
	if err := s.SetUserActive(1, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deactivating the only admin: %v", err)
	}
	second, err := s.CreateUser("admin2", "Admin 2", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserActive(1, false); err != nil {
		t.Fatalf("deactivate first admin: %v", err)
	}
	if err := s.SetUserActive(second, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deactivating the last admin: %v", err)
	}
}

func TestClaimOrderIsAtomic(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)
	if _, err := s.CreateUser("mia", "Mia", "hash", "user"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimOrder(id, 2); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := s.ClaimOrder(id, 3); !errors.Is(err, ErrConflict) {
		t.Fatalf("second claim should conflict, got %v", err)
	}
	o, err := s.Order(id)
	if err != nil || o.AssigneeID != 2 {
		t.Fatalf("assignee after claim: %+v, %v", o, err)
	}
}

func TestInterruptedFlagClearsAfterResume(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)
	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecoverTimers(); err != nil {
		t.Fatal(err)
	}
	o, err := s.Order(id)
	if err != nil || !o.Interrupted {
		t.Fatalf("recovered order should be flagged: %+v, %v", o, err)
	}
	if err := s.TimerAction(id, 1, "resume"); err != nil {
		t.Fatal(err)
	}
	o, err = s.Order(id)
	if err != nil || o.Interrupted {
		t.Fatalf("flag should be cleared after resume: %+v, %v", o, err)
	}
}

func TestSQLiteDSNKeepsRelativePathsValid(t *testing.T) {
	want := "file:./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if got := sqliteDSN("./data/app.db"); got != want {
		t.Fatalf("relative DSN: got %q, want %q", got, want)
	}
	if got := sqliteDSN("data/app.db"); got != "file:data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)" {
		t.Fatalf("relative DSN without dot: %q", got)
	}
	if got := sqliteDSN(":memory:"); got != ":memory:" {
		t.Fatalf("memory DSN: %q", got)
	}
}

func TestOpenRelativeDatabasePath(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	s, err := Open("./data/app.db")
	if err != nil {
		t.Fatalf("open relative database: %v", err)
	}
	defer s.Close()
	if _, err := os.Stat("data/app.db"); err != nil {
		t.Fatalf("database file missing: %v", err)
	}
}

func TestFileDatabaseAppliesPerConnectionPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open file database: %v", err)
	}
	defer s.Close()
	var foreignKeys, busyTimeout int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 {
		t.Fatalf("pragmas: foreign_keys=%d busy_timeout=%d", foreignKeys, busyTimeout)
	}
}
