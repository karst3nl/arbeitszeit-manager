-- SPDX-License-Identifier: AGPL-3.0-only

-- An explicit Soll for the whole order. 0 means: use the sum of task minutes.
ALTER TABLE work_orders
    ADD COLUMN planned_minutes INTEGER NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0);
