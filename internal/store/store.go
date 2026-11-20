// SPDX-License-Identifier: AGPL-3.0-only

// Package store owns all database access and timer transactions.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/karst3nl/arbeitszeit-manager/internal/model"
	"github.com/karst3nl/arbeitszeit-manager/internal/timer"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrConflict is returned when a timer action does not match the current state.
var ErrConflict = errors.New("timer state conflict")

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// ErrLastAdmin is returned when the last active admin would be deactivated.
var ErrLastAdmin = errors.New("last active admin")

// Store wraps the SQLite connection.
type Store struct {
	db *sql.DB
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryRow(query string, args ...any) *sql.Row
}

// sqliteDSN builds a file URI with the per-connection pragmas. OmitHost is
// essential: without it net/url turns "./data/app.db" into "file://./data/…",
// whose authority "." SQLite rejects as an invalid URI.
func sqliteDSN(path string) string {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return path
	}
	return (&url.URL{
		Scheme:   "file",
		OmitHost: true,
		Path:     path,
		RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
	}).String()
}

// Open opens the database, applies pragmas and runs pending migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	// foreign_keys and busy_timeout are per-connection settings. Putting them
	// in the DSN ensures they also apply to a replacement connection created
	// after a driver error. journal_mode and synchronous are persistent.
	// In-memory databases use the plain SQLite name; the pragmas are then
	// applied below instead.
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite has a single writer. Serialising access keeps the Pi simple and
	// avoids SQLITE_BUSY without giving up correctness for six users.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s (check that the database and its directory belong to the service user): %w", p, err)
		}
	}

	s := &Store{db: db}
	if err := s.Migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database (check that the database and its directory belong to the service user): %w", err)
	}
	return s, nil
}

// Migrate applies all embedded migration files in filename order.
func (s *Store) Migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("invalid migration name %q: %w", entry.Name(), err)
		}

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		sqlBytes, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().Unix()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Backup writes a consistent snapshot of the database with VACUUM INTO.
func (s *Store) Backup(path string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}
	}
	// VACUUM INTO expects a string literal, so the path is quoted manually.
	quoted := strings.ReplaceAll(path, "'", "''")
	if _, err := s.db.Exec("VACUUM INTO '" + quoted + "'"); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// Ping verifies the database connection.
func (s *Store) Ping() error { return s.db.Ping() }

// RecoverTimers closes segments left open by a crash or reboot. The last
// heartbeat timestamp is used, so at most one heartbeat interval (10 seconds)
// is lost.
func (s *Store) RecoverTimers() (int, error) {
	res, err := s.db.Exec(`
		UPDATE time_segments
		   SET end_at = MAX(start_at, COALESCE(
		           (SELECT timer_last_seen FROM work_orders WHERE work_orders.id = time_segments.work_order_id),
		           start_at)),
		       interrupted = 1,
		       ended_by = started_by
		 WHERE end_at IS NULL`)
	if err != nil {
		return 0, err
	}
	count, _ := res.RowsAffected()
	if _, err := s.db.Exec(`UPDATE work_orders SET timer_state = 'paused' WHERE timer_state = 'running'`); err != nil {
		return 0, err
	}
	return int(count), nil
}

// --- Users -----------------------------------------------------------------

const userColumns = `id, username, display_name, role, active, created_at, last_login_at`

func scanUser(row interface{ Scan(...any) error }) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Active, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUser inserts a new user and returns its ID.
func (s *Store) CreateUser(username, displayName, passwordHash, role string) (int64, error) {
	if displayName == "" {
		displayName = username
	}
	res, err := s.db.Exec(`INSERT INTO users(username, display_name, password_hash, role, created_at)
		VALUES(?, ?, ?, ?, ?)`, username, displayName, passwordHash, role, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UserByID looks up a user by ID.
func (s *Store) UserByID(id int64) (*model.User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// UserByUsername looks up a user by name (case insensitive), including the
// password hash needed for login.
func (s *Store) UserByUsername(username string) (*model.User, error) {
	var u model.User
	err := s.db.QueryRow(`SELECT id, username, display_name, role, active, created_at, last_login_at, password_hash
		FROM users WHERE username = ? COLLATE NOCASE`, username).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Active, &u.CreatedAt, &u.LastLoginAt, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ActiveUser reports whether the given user exists and is active.
func (s *Store) ActiveUser(id int64) (bool, error) {
	var active int
	err := s.db.QueryRow(`SELECT active FROM users WHERE id = ?`, id).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return active == 1, nil
}

// AWMinutes returns the configured number of minutes per Arbeitswert.
func (s *Store) AWMinutes() (float64, error) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'aw_minutes'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return timer.DefaultMinutesPerAW, nil
	}
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return timer.DefaultMinutesPerAW, nil
	}
	return timer.NormalizeMinutesPerAW(value), nil
}

// SetAWMinutes stores the configured number of minutes per Arbeitswert.
func (s *Store) SetAWMinutes(value float64) error {
	raw := strconv.FormatFloat(timer.NormalizeMinutesPerAW(value), 'f', -1, 64)
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES('aw_minutes', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, raw)
	return err
}

// Users lists all users, active ones first.
func (s *Store) Users() ([]model.User, error) {
	rows, err := s.db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY active DESC, username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// SetUserActive enables or disables a user. Deactivating the last active
// admin is rejected so the application can always be administered.
func (s *Store) SetUserActive(id int64, active bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if !active {
		var role string
		var isActive bool
		if err := tx.QueryRow(`SELECT role, active FROM users WHERE id = ?`, id).Scan(&role, &isActive); err != nil {
			return err
		}
		if role == "admin" && isActive {
			var count int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = 1`).Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return ErrLastAdmin
			}
		}
	}
	if _, err := tx.Exec(`UPDATE users SET active = ? WHERE id = ?`, active, id); err != nil {
		return err
	}
	return tx.Commit()
}

// IsActiveUser reports whether the ID belongs to an account that can log in.
func (s *Store) IsActiveUser(id int64) (bool, error) {
	var active int
	err := s.db.QueryRow(`SELECT active FROM users WHERE id = ?`, id).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && active == 1, err
}

// TouchLogin records the last successful login.
func (s *Store) TouchLogin(id int64) error {
	_, err := s.db.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

// ChangePassword replaces the password hash and invalidates all sessions of
// the user in one transaction, so old credentials cannot stay valid.
func (s *Store) ChangePassword(id int64, hash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetDisplayName updates the name shown in the interface.
func (s *Store) SetDisplayName(id int64, name string) error {
	_, err := s.db.Exec(`UPDATE users SET display_name = ? WHERE id = ?`, name, id)
	return err
}

// --- Sessions --------------------------------------------------------------

// CreateSession stores the hash of a session token.
func (s *Store) CreateSession(userID int64, tokenHash string, expiresAt int64) error {
	_, err := s.db.Exec(`INSERT INTO sessions(user_id, token_hash, expires_at, created_at) VALUES(?, ?, ?, ?)`,
		userID, tokenHash, expiresAt, time.Now().Unix())
	return err
}

// SessionUser returns the user belonging to a valid, unexpired token hash.
func (s *Store) SessionUser(tokenHash string) (*model.User, error) {
	return scanUser(s.db.QueryRow(`
		SELECT u.id, u.username, u.display_name, u.role, u.active, u.created_at, u.last_login_at
		  FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ? AND s.expires_at > ? AND u.active = 1`, tokenHash, time.Now().Unix()))
}

// DeleteSession removes a session by token hash.
func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// PurgeExpiredSessions removes stale sessions.
func (s *Store) PurgeExpiredSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	return err
}

// --- Orders and tasks ------------------------------------------------------

// orderSelect takes the current Unix timestamp as its first argument so the
// "Ist" value is live even while a segment is still running.
const orderSelect = `
	SELECT o.id, o.title, o.description, o.status,
	       o.owner_id, COALESCE(ow.display_name, ''),
	       o.assignee_id, COALESCE(aw.display_name, ''), o.assigned_at,
	       o.timer_state, o.timer_started_at, o.created_at, o.completed_at, o.notes,
	       o.planned_minutes,
	       COALESCE((SELECT SUM(COALESCE(end_at, ?) - start_at) FROM time_segments
	                  WHERE work_order_id = o.id), 0),
	       EXISTS(SELECT 1 FROM time_segments WHERE work_order_id = o.id AND interrupted = 1)
	  FROM work_orders o
	  LEFT JOIN users ow ON ow.id = o.owner_id
	  LEFT JOIN users aw ON aw.id = o.assignee_id`

func scanOrder(row interface{ Scan(...any) error }) (*model.Order, error) {
	var o model.Order
	err := row.Scan(&o.ID, &o.Title, &o.Description, &o.Status,
		&o.OwnerID, &o.OwnerName, &o.AssigneeID, &o.AssigneeName, &o.AssignedAt,
		&o.TimerState, &o.TimerStartedAt, &o.CreatedAt, &o.CompletedAt, &o.Notes,
		&o.PlannedMinutes, &o.ActualSeconds, &o.Interrupted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// Orders lists all work orders, newest first.
func (s *Store) Orders() ([]model.Order, error) {
	rows, err := s.db.Query(orderSelect+` ORDER BY o.id DESC`, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []model.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, *o)
	}
	return orders, rows.Err()
}

// Order looks up one work order.
func (s *Store) Order(id int64) (*model.Order, error) {
	return scanOrder(s.db.QueryRow(orderSelect+` WHERE o.id = ?`, time.Now().Unix(), id))
}

// CreateOrder inserts a new work order with the Soll and assignee chosen by
// its creator.
func (s *Store) CreateOrder(title, description string, ownerID, assigneeID, plannedMinutes int64) (int64, error) {
	now := time.Now().Unix()
	var assignedAt int64
	if assigneeID > 0 {
		assignedAt = now
	}
	res, err := s.db.Exec(`INSERT INTO work_orders(title, description, planned_minutes, owner_id, assignee_id, assigned_at, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, title, description, plannedMinutes, ownerID, assigneeID, assignedAt, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetAssignee assigns an order to a user (0 clears the assignment). Changing
// the assignee restarts the handover clock.
func (s *Store) SetAssignee(orderID, userID int64) error {
	_, err := s.db.Exec(`UPDATE work_orders
		SET assignee_id = ?,
		    assigned_at = CASE WHEN assignee_id = ? THEN assigned_at
		                       WHEN ? = 0 THEN 0 ELSE ? END
		WHERE id = ?`, userID, userID, userID, time.Now().Unix(), orderID)
	return err
}

// SetNotes stores the evaluation notes.
func (s *Store) SetNotes(orderID int64, notes string) error {
	_, err := s.db.Exec(`UPDATE work_orders SET notes = ? WHERE id = ?`, notes, orderID)
	return err
}

// DeleteOrder removes an order and all of its tasks, time segments and
// evaluations via ON DELETE CASCADE.
func (s *Store) DeleteOrder(orderID int64) error {
	_, err := s.db.Exec(`DELETE FROM work_orders WHERE id = ?`, orderID)
	return err
}

// Tasks returns the tasks of an order in their configured order.
func (s *Store) Tasks(orderID int64) ([]model.Task, error) {
	rows, err := s.db.Query(`SELECT id, work_order_id, title, position, done
		FROM tasks WHERE work_order_id = ? ORDER BY position, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.OrderID, &t.Title, &t.Position, &t.Done); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// TaskInOrder reports whether a task belongs to the given order.
func (s *Store) TaskInOrder(orderID, taskID int64) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM tasks WHERE id = ? AND work_order_id = ?`, taskID, orderID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// AddTask appends a task to an order.
func (s *Store) AddTask(orderID int64, title string) error {
	var next int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM tasks WHERE work_order_id = ?`, orderID).Scan(&next); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO tasks(work_order_id, title, position) VALUES(?, ?, ?)`,
		orderID, title, next)
	return err
}

// UpdateTask changes title and done flag of a task. The work order ID is part
// of the WHERE clause so a task cannot be modified through a different order's
// URL.
func (s *Store) UpdateTask(orderID, id int64, title string, done bool) error {
	res, err := s.db.Exec(`UPDATE tasks SET title = ?, done = ?
		WHERE id = ? AND work_order_id = ?`, title, done, id, orderID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTaskDone changes only the completion flag of a task. It lets assigned
// users track progress without giving them access to the task title.
func (s *Store) SetTaskDone(orderID, id int64, done bool) error {
	res, err := s.db.Exec(`UPDATE tasks SET done = ?
		WHERE id = ? AND work_order_id = ?`, done, id, orderID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTask removes a task, scoped to its work order.
func (s *Store) DeleteTask(orderID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id = ? AND work_order_id = ?`, id, orderID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Task templates --------------------------------------------------------

// TaskTemplates returns the admin-maintained templates for the order form.
func (s *Store) TaskTemplates() ([]model.TaskTemplate, error) {
	rows, err := s.db.Query(`SELECT id, title, created_at
		FROM task_templates ORDER BY title COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []model.TaskTemplate
	for rows.Next() {
		var t model.TaskTemplate
		if err := rows.Scan(&t.ID, &t.Title, &t.CreatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// CreateTaskTemplate adds a reusable task. Titles are unique to keep the
// dropdown readable. The legacy planned_minutes column stays at its default.
func (s *Store) CreateTaskTemplate(title string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO task_templates(title, created_at) VALUES(?, ?)`,
		title, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateTaskTemplate renames a template. Existing orders keep their own copy
// of the task title.
func (s *Store) UpdateTaskTemplate(id int64, title string) error {
	res, err := s.db.Exec(`UPDATE task_templates SET title = ? WHERE id = ?`, title, id)
	if err != nil {
		return err
	}
	if !changed(res) {
		return ErrNotFound
	}
	return nil
}

// DeleteTaskTemplate removes a template. Existing orders are unaffected
// because they hold their own copy of the task.
func (s *Store) DeleteTaskTemplate(id int64) error {
	res, err := s.db.Exec(`DELETE FROM task_templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateOrderFromTemplates creates an order and copies every selected task
// template into it in one transaction. A missing template aborts the whole
// operation, so no half-created order is left behind.
func (s *Store) CreateOrderFromTemplates(title, description string, ownerID, assigneeID, plannedMinutes int64, templateIDs []int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Read all templates before inserting anything so an unknown ID cannot
	// leave an order with only some of the chosen tasks.
	tasks := make([]model.TaskTemplate, 0, len(templateIDs))
	for _, id := range templateIDs {
		var t model.TaskTemplate
		err := tx.QueryRow(`SELECT id, title FROM task_templates WHERE id = ?`, id).
			Scan(&t.ID, &t.Title)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, err
		}
		tasks = append(tasks, t)
	}

	now := time.Now().Unix()
	var assignedAt int64
	if assigneeID > 0 {
		assignedAt = now
	}
	res, err := tx.Exec(`INSERT INTO work_orders(title, description, planned_minutes, owner_id, assignee_id, assigned_at, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, title, description, plannedMinutes, ownerID, assigneeID, assignedAt, now)
	if err != nil {
		return 0, err
	}
	orderID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for position, t := range tasks {
		if _, err := tx.Exec(`INSERT INTO tasks(work_order_id, title, position)
			VALUES(?, ?, ?)`, orderID, t.Title, position); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return orderID, nil
}

// --- Timer -----------------------------------------------------------------

func changed(res sql.Result) bool {
	if res == nil {
		return false
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1
}

func closeSegment(tx *sql.Tx, orderID, now, userID int64) error {
	_, err := tx.Exec(`UPDATE time_segments SET end_at = ?, ended_by = ?
		WHERE work_order_id = ? AND end_at IS NULL`, now, userID, orderID)
	return err
}

func closePause(tx *sql.Tx, orderID, now, userID int64) error {
	_, err := tx.Exec(`UPDATE pause_segments SET end_at = ?, ended_by = ?
		WHERE work_order_id = ? AND end_at IS NULL`, now, userID, orderID)
	return err
}

// TimerAction performs a state transition atomically. Valid actions are
// start, pause, resume, stop and reopen.
func (s *Store) TimerAction(orderID, userID int64, action string) error {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var res sql.Result
	switch action {
	case "start", "resume":
		want := "stopped"
		if action == "resume" {
			want = "paused"
		}
		res, err = tx.Exec(`UPDATE work_orders
			SET timer_state = 'running', timer_started_at = ?, timer_started_by = ?, timer_last_seen = ?
			WHERE id = ? AND timer_state = ?`, now, userID, now, orderID, want)
		if err == nil && changed(res) {
			_, err = tx.Exec(`INSERT INTO time_segments(work_order_id, start_at, started_by) VALUES(?, ?, ?)`,
				orderID, now, userID)
			if err == nil {
				err = closePause(tx, orderID, now, userID)
			}
		}

	case "pause":
		res, err = tx.Exec(`UPDATE work_orders SET timer_state = 'paused', timer_last_seen = ?
			WHERE id = ? AND timer_state = 'running'`, now, orderID)
		if err == nil && changed(res) {
			err = closeSegment(tx, orderID, now, userID)
			if err == nil {
				_, err = tx.Exec(`INSERT INTO pause_segments(work_order_id, start_at, started_by) VALUES(?, ?, ?)`,
					orderID, now, userID)
			}
		}

	case "stop":
		res, err = tx.Exec(`UPDATE work_orders
			SET timer_state = 'stopped', status = 'done', completed_at = ?, timer_last_seen = ?
			WHERE id = ? AND timer_state IN ('running', 'paused')`, now, now, orderID)
		if err == nil && changed(res) {
			if err = closeSegment(tx, orderID, now, userID); err != nil {
				break
			}
			if err = closePause(tx, orderID, now, userID); err != nil {
				break
			}
			var report *model.Report
			report, err = buildReport(context.Background(), tx, orderID, now)
			if err != nil {
				break
			}
			_, err = tx.Exec(`INSERT INTO evaluations(work_order_id, planned_minutes, actual_seconds,
				deviation_seconds, deviation_percent, summary, created_at)
				VALUES(?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(work_order_id) DO UPDATE SET
					planned_minutes = excluded.planned_minutes,
					actual_seconds = excluded.actual_seconds,
					deviation_seconds = excluded.deviation_seconds,
					deviation_percent = excluded.deviation_percent,
					summary = excluded.summary,
					created_at = excluded.created_at`,
				orderID, report.PlannedMinutes, report.ActualSeconds, report.DeviationSeconds,
				report.DeviationPercent, report.Summary, now)
		}

	case "reopen":
		res, err = tx.Exec(`UPDATE work_orders
			SET timer_state = 'stopped', status = 'active', completed_at = 0
			WHERE id = ? AND status = 'done'`, orderID)
		if err == nil && changed(res) {
			_, err = tx.Exec(`DELETE FROM evaluations WHERE work_order_id = ?`, orderID)
		}

	default:
		return fmt.Errorf("unknown timer action %q", action)
	}

	// A corrected segment is only flagged until the timer is continued or
	// finished normally; then the correction badge can disappear again.
	if err == nil && changed(res) && (action == "resume" || action == "stop") {
		_, err = tx.Exec(`UPDATE time_segments SET interrupted = 0 WHERE work_order_id = ?`, orderID)
	}

	if err != nil {
		return err
	}
	if !changed(res) {
		return ErrConflict
	}
	return tx.Commit()
}

// TouchTimer records a heartbeat for a running order.
func (s *Store) TouchTimer(orderID int64) error {
	_, err := s.db.Exec(`UPDATE work_orders SET timer_last_seen = ?
		WHERE id = ? AND timer_state = 'running'`, time.Now().Unix(), orderID)
	return err
}

// TouchRunningTimers is called by a background ticker so a running timer stays
// recoverable even when no browser has the order page open.
func (s *Store) TouchRunningTimers() error {
	_, err := s.db.Exec(`UPDATE work_orders SET timer_last_seen = ? WHERE timer_state = 'running'`, time.Now().Unix())
	return err
}

// TimerSnapshot is the server-side source of truth for the live timer.
type TimerSnapshot struct {
	State   string
	Elapsed int64
	Since   int64
}

// TimerSnapshot returns the current state and elapsed seconds of an order.
func (s *Store) TimerSnapshot(orderID int64) (TimerSnapshot, error) {
	var snap TimerSnapshot
	var startedAt int64
	err := s.db.QueryRow(`SELECT timer_state, timer_started_at FROM work_orders WHERE id = ?`, orderID).
		Scan(&snap.State, &startedAt)
	if err != nil {
		return snap, err
	}

	var base int64
	err = s.db.QueryRow(`SELECT COALESCE(SUM(end_at - start_at), 0) FROM time_segments
		WHERE work_order_id = ? AND end_at IS NOT NULL`, orderID).Scan(&base)
	if err != nil {
		return snap, err
	}
	snap.Elapsed = base
	if snap.State == "running" {
		snap.Since = startedAt
	}
	return snap, nil
}

// --- Timeline and reports --------------------------------------------------

const timelineSelect = `
	SELECT id, kind, start_at, end_at,
	       CASE WHEN end_at IS NULL THEN MAX(0, ? - start_at)
	            ELSE MAX(0, end_at - start_at) END,
	       end_at IS NULL
	  FROM (
	    SELECT id, 'work' AS kind, start_at, end_at
	      FROM time_segments WHERE work_order_id = ?
	    UNION ALL
	    SELECT id, 'pause' AS kind, start_at, end_at
	      FROM pause_segments WHERE work_order_id = ?
	  )
	 ORDER BY start_at, CASE WHEN kind = 'work' THEN 0 ELSE 1 END, id`

func timelineEntries(ctx context.Context, q queryer, orderID, now int64) ([]model.TimeEntry, error) {
	rows, err := q.QueryContext(ctx, timelineSelect, now, orderID, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.TimeEntry
	for rows.Next() {
		var e model.TimeEntry
		var endAt sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Kind, &e.StartAt, &endAt, &e.DurationSeconds, &e.Open); err != nil {
			return nil, err
		}
		if endAt.Valid {
			e.EndAt = endAt.Int64
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Timeline returns every work and pause interval of an order, oldest first.
func (s *Store) Timeline(orderID int64) ([]model.TimeEntry, error) {
	return timelineEntries(context.Background(), s.db, orderID, time.Now().Unix())
}

// fillTimeline adds handover, start, pause and completion information to a
// report, regardless of whether the values were computed or stored earlier.
func fillTimeline(ctx context.Context, q queryer, r *model.Report, now int64) error {
	err := q.QueryRowContext(ctx, `SELECT COALESCE(assigned_at, 0), COALESCE(completed_at, 0)
		FROM work_orders WHERE id = ?`, r.OrderID).
		Scan(&r.AssignedAt, &r.CompletedAt)
	if err != nil {
		return err
	}
	r.CreatedAt = r.CompletedAt

	r.Timeline, err = timelineEntries(ctx, q, r.OrderID, now)
	if err != nil {
		return err
	}
	for _, e := range r.Timeline {
		if e.Kind == "pause" {
			r.PauseCount++
			r.PauseSeconds += e.DurationSeconds
			continue
		}
		if r.StartedAt == 0 {
			r.StartedAt = e.StartAt
		}
	}
	return nil
}

func buildReport(ctx context.Context, q queryer, orderID, now int64) (*model.Report, error) {
	var r model.Report
	err := q.QueryRow(`SELECT id, title, notes,
		planned_minutes,
		COALESCE((SELECT SUM(COALESCE(end_at, ?) - start_at) FROM time_segments
		          WHERE work_order_id = work_orders.id), 0)
		FROM work_orders WHERE id = ?`, now, orderID).
		Scan(&r.OrderID, &r.Title, &r.Notes, &r.PlannedMinutes, &r.ActualSeconds)
	if err != nil {
		return nil, err
	}
	if err := fillTimeline(ctx, q, &r, now); err != nil {
		return nil, err
	}
	r.DeviationSeconds = r.ActualSeconds - r.PlannedMinutes*60
	if r.PlannedMinutes > 0 {
		r.DeviationPercent = float64(r.DeviationSeconds) / float64(r.PlannedMinutes*60) * 100
	}
	switch {
	case r.DeviationSeconds == 0:
		r.Summary = "Genau im Soll."
	case r.DeviationSeconds > 0:
		r.Summary = fmt.Sprintf("Ist-Zeit liegt %s über dem Soll.", timer.Duration(r.DeviationSeconds))
	default:
		r.Summary = fmt.Sprintf("Ist-Zeit liegt %s unter dem Soll.", timer.Duration(-r.DeviationSeconds))
	}

	rows, err := q.QueryContext(ctx, `SELECT id, work_order_id, title, position, done
		FROM tasks WHERE work_order_id = ? ORDER BY position, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.OrderID, &t.Title, &t.Position, &t.Done); err != nil {
			return nil, err
		}
		r.Tasks = append(r.Tasks, t)
	}
	return &r, rows.Err()
}

// Report returns a stored evaluation or computes the current one on the fly.
func (s *Store) Report(orderID int64) (*model.Report, error) {
	var r model.Report
	err := s.db.QueryRow(`SELECT work_order_id, planned_minutes, actual_seconds,
		deviation_seconds, deviation_percent, summary, created_at
		FROM evaluations WHERE work_order_id = ?`, orderID).
		Scan(&r.OrderID, &r.PlannedMinutes, &r.ActualSeconds, &r.DeviationSeconds,
			&r.DeviationPercent, &r.Summary, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return buildReport(context.Background(), s.db, orderID, time.Now().Unix())
	}
	if err != nil {
		return nil, err
	}
	var notes string
	if err := s.db.QueryRow(`SELECT title, notes FROM work_orders WHERE id = ?`, orderID).Scan(&r.Title, &notes); err != nil {
		return nil, err
	}
	r.Notes = notes
	if err := fillTimeline(context.Background(), s.db, &r, time.Now().Unix()); err != nil {
		return nil, err
	}
	r.Tasks, err = s.Tasks(orderID)
	return &r, err
}
