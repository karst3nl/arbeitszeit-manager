-- SPDX-License-Identifier: AGPL-3.0-only

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    active        INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS work_orders (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    title            TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'done')),
    owner_id         INTEGER NOT NULL REFERENCES users(id),
    assignee_id      INTEGER NOT NULL DEFAULT 0,
    timer_state      TEXT NOT NULL DEFAULT 'stopped' CHECK (timer_state IN ('stopped', 'running', 'paused')),
    timer_started_at INTEGER NOT NULL DEFAULT 0,
    timer_started_by INTEGER NOT NULL DEFAULT 0,
    timer_last_seen  INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    completed_at     INTEGER NOT NULL DEFAULT 0,
    notes            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_orders_status ON work_orders(status);

CREATE TABLE IF NOT EXISTS tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id   INTEGER NOT NULL REFERENCES work_orders(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    planned_minutes INTEGER NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0),
    position        INTEGER NOT NULL DEFAULT 0,
    done            INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_order ON tasks(work_order_id, position);

CREATE TABLE IF NOT EXISTS time_segments (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL REFERENCES work_orders(id) ON DELETE CASCADE,
    start_at      INTEGER NOT NULL,
    end_at        INTEGER,
    started_by    INTEGER NOT NULL REFERENCES users(id),
    ended_by      INTEGER REFERENCES users(id),
    interrupted   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS one_open_segment_per_order
    ON time_segments(work_order_id) WHERE end_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_segments_order ON time_segments(work_order_id, start_at);

CREATE TABLE IF NOT EXISTS evaluations (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id     INTEGER NOT NULL UNIQUE REFERENCES work_orders(id) ON DELETE CASCADE,
    planned_minutes   INTEGER NOT NULL,
    actual_seconds    INTEGER NOT NULL,
    deviation_seconds INTEGER NOT NULL,
    deviation_percent REAL NOT NULL,
    summary           TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL
);
