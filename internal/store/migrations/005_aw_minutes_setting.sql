-- SPDX-License-Identifier: AGPL-3.0-only

-- The AW conversion factor is configurable in the admin area. The default of
-- six minutes keeps the previous behaviour: ten AW are one hour.
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO settings(key, value) VALUES ('aw_minutes', '6');
