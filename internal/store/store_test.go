// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/karst3nl/arbeitszeit-manager/internal/model"
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
	id, err := s.CreateOrder("Testauftrag", "Beschreibung", 1, 1, 180)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := s.AddTask(id, "Konzeption"); err != nil {
		t.Fatalf("add task 1: %v", err)
	}
	if err := s.AddTask(id, "Umsetzung"); err != nil {
		t.Fatalf("add task 2: %v", err)
	}
	return id
}

func TestOrderSollIsExplicit(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)

	o, err := s.Order(id)
	if err != nil || o.PlannedMinutes != 180 {
		t.Fatalf("explicit Soll: %+v, %v", o, err)
	}
	rep, err := s.Report(id)
	if err != nil || rep.PlannedMinutes != 180 {
		t.Fatalf("report Soll: %+v, %v", rep, err)
	}

	// Tasks no longer carry times: adding one must not change the Soll.
	if err := s.AddTask(id, "Prüfung"); err != nil {
		t.Fatal(err)
	}
	o, err = s.Order(id)
	if err != nil || o.PlannedMinutes != 180 {
		t.Fatalf("Soll changed after adding a task: %+v, %v", o, err)
	}

	if _, err := s.db.Exec(`UPDATE work_orders SET planned_minutes = 90 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	o, err = s.Order(id)
	if err != nil || o.PlannedMinutes != 90 {
		t.Fatalf("updated Soll not used: %+v, %v", o, err)
	}
	rep, err = s.Report(id)
	if err != nil || rep.PlannedMinutes != 90 {
		t.Fatalf("updated Soll missing in report: %+v, %v", rep, err)
	}
}

func TestSetTaskDoneKeepsTitle(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)

	if err := s.SetTaskDone(id, 1, true); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Tasks(id)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks: %+v, %v", tasks, err)
	}
	if tasks[0].Title != "Konzeption" || !tasks[0].Done || tasks[1].Done {
		t.Fatalf("unexpected task state: %+v", tasks)
	}
}

func TestTaskTemplatesAndOrderCreation(t *testing.T) {
	s := testStore(t)
	templateID, err := s.CreateTaskTemplate("Konzeption")
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := s.CreateTaskTemplate("Umsetzung")
	if err != nil {
		t.Fatal(err)
	}
	templates, err := s.TaskTemplates()
	if err != nil || len(templates) != 2 {
		t.Fatalf("templates: %d, %v", len(templates), err)
	}
	if templates[0].Title != "Konzeption" {
		t.Fatalf("unexpected template: %+v", templates[0])
	}

	orderID, err := s.CreateOrderFromTemplates("Kunde", "", 1, 2, 180, []int64{templateID, secondID})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Tasks(orderID)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("copied tasks: %d, %v", len(tasks), err)
	}
	if tasks[0].Title != "Konzeption" || tasks[1].Title != "Umsetzung" {
		t.Fatalf("unexpected copied tasks: %+v", tasks)
	}
	o, err := s.Order(orderID)
	if err != nil || o.PlannedMinutes != 180 {
		t.Fatalf("order Soll from templates: %+v, %v", o, err)
	}
	if o.AssigneeID != 2 {
		t.Fatalf("order assignee from creation: %d", o.AssigneeID)
	}

	before, err := s.Orders()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrderFromTemplates("Fehler", "", 1, 2, 60, []int64{templateID, 999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing template: %v", err)
	}
	after, err := s.Orders()
	if err != nil || len(after) != len(before) {
		t.Fatalf("half-created order left behind: %d -> %d, %v", len(before), len(after), err)
	}

	if err := s.DeleteTaskTemplate(templateID); err != nil {
		t.Fatal(err)
	}
	templates, err = s.TaskTemplates()
	if err != nil || len(templates) != 1 {
		t.Fatalf("templates after delete: %d, %v", len(templates), err)
	}
	tasks, err = s.Tasks(orderID)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("copied tasks should survive template deletion: %d, %v", len(tasks), err)
	}

	if err := s.UpdateTaskTemplate(secondID, "Umsetzung neu"); err != nil {
		t.Fatalf("rename template: %v", err)
	}
	templates, err = s.TaskTemplates()
	if err != nil || len(templates) != 1 || templates[0].Title != "Umsetzung neu" {
		t.Fatalf("renamed template missing: %+v, %v", templates, err)
	}
	if err := s.UpdateTaskTemplate(999, "Gibt es nicht"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renaming a missing template: %v", err)
	}

	active, err := s.ActiveUser(1)
	if err != nil || !active {
		t.Fatalf("active user: %v, %v", active, err)
	}
	active, err = s.ActiveUser(999)
	if err != nil || active {
		t.Fatalf("missing user reported active: %v, %v", active, err)
	}
}

func TestAWMinutesSetting(t *testing.T) {
	s := testStore(t)

	factor, err := s.AWMinutes()
	if err != nil || factor != 6 {
		t.Fatalf("default AW factor: %v, %v", factor, err)
	}
	if err := s.SetAWMinutes(7.5); err != nil {
		t.Fatalf("save AW factor: %v", err)
	}
	factor, err = s.AWMinutes()
	if err != nil || factor != 7.5 {
		t.Fatalf("saved AW factor: %v, %v", factor, err)
	}

	// An invalid stored value falls back instead of breaking every page.
	if _, err := s.db.Exec(`UPDATE settings SET value = 'nope' WHERE key = 'aw_minutes'`); err != nil {
		t.Fatal(err)
	}
	factor, err = s.AWMinutes()
	if err != nil || factor != 6 {
		t.Fatalf("fallback AW factor: %v, %v", factor, err)
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

func TestOrderTimelineRecordsHandoverStartPausesAndEnd(t *testing.T) {
	s := testStore(t)
	id := setupOrder(t, s)

	// Changing the assignee restarts the handover clock.
	if _, err := s.db.Exec(`UPDATE work_orders SET assigned_at = 100 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	mia, err := s.CreateUser("mia", "Mia", "hash", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssignee(id, mia); err != nil {
		t.Fatal(err)
	}
	o, err := s.Order(id)
	if err != nil || o.AssignedAt <= 100 || o.AssigneeID != mia {
		t.Fatalf("handover timestamp: %+v, %v", o, err)
	}

	if err := s.TimerAction(id, 1, "start"); err != nil {
		t.Fatal(err)
	}
	if err := s.TimerAction(id, 1, "pause"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE pause_segments SET start_at = start_at - 30 WHERE work_order_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	openTimeline, err := s.Timeline(id)
	if err != nil || len(openTimeline) != 2 {
		t.Fatalf("open timeline: %+v, %v", openTimeline, err)
	}
	var openPause *model.TimeEntry
	for i := range openTimeline {
		if openTimeline[i].Kind == "pause" {
			openPause = &openTimeline[i]
			break
		}
	}
	if openPause == nil || !openPause.Open || openPause.DurationSeconds < 30 {
		t.Fatalf("open pause missing: %+v", openTimeline)
	}

	if err := s.TimerAction(id, 1, "resume"); err != nil {
		t.Fatal(err)
	}
	if err := s.TimerAction(id, 1, "stop"); err != nil {
		t.Fatal(err)
	}

	// Use deterministic timestamps so durations are exact regardless of how
	// fast the test runs.
	if _, err := s.db.Exec(`UPDATE work_orders SET assigned_at = 900, completed_at = 2020 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE time_segments SET start_at = 1000, end_at = 1600 WHERE work_order_id = ? AND id = 1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE pause_segments SET start_at = 1600, end_at = 1720 WHERE work_order_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE time_segments SET start_at = 1720, end_at = 2020 WHERE work_order_id = ? AND id = 2`, id); err != nil {
		t.Fatal(err)
	}

	timeline, err := s.Timeline(id)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.TimeEntry{
		{ID: 1, Kind: "work", StartAt: 1000, EndAt: 1600, DurationSeconds: 600},
		{ID: 1, Kind: "pause", StartAt: 1600, EndAt: 1720, DurationSeconds: 120},
		{ID: 2, Kind: "work", StartAt: 1720, EndAt: 2020, DurationSeconds: 300},
	}
	if len(timeline) != len(want) {
		t.Fatalf("timeline length = %d, want %d: %+v", len(timeline), len(want), timeline)
	}
	for i, entry := range timeline {
		if entry.ID != want[i].ID || entry.Kind != want[i].Kind ||
			entry.StartAt != want[i].StartAt || entry.EndAt != want[i].EndAt ||
			entry.DurationSeconds != want[i].DurationSeconds || entry.Open {
			t.Errorf("timeline[%d] = %+v, want %+v", i, entry, want[i])
		}
	}

	rep, err := s.Report(id)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AssignedAt != 900 || rep.StartedAt != 1000 || rep.CompletedAt != 2020 ||
		rep.PauseCount != 1 || rep.PauseSeconds != 120 {
		t.Fatalf("report timeline: %+v", rep)
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
