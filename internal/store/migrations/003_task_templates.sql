-- SPDX-License-Identifier: AGPL-3.0-only

-- Reusable tasks maintained by admins and offered when an order is created.
CREATE TABLE IF NOT EXISTS task_templates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    title           TEXT NOT NULL UNIQUE COLLATE NOCASE,
    planned_minutes INTEGER NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0),
    created_at      INTEGER NOT NULL
);
