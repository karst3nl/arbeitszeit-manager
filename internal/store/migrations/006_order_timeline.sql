-- SPDX-License-Identifier: AGPL-3.0-only

-- Record when an order was handed to an employee and keep explicit pause
-- intervals so the timeline can show starts, pauses and the end time.
ALTER TABLE work_orders ADD COLUMN assigned_at INTEGER NOT NULL DEFAULT 0;
UPDATE work_orders SET assigned_at = created_at
 WHERE assignee_id <> 0 AND assigned_at = 0;

CREATE TABLE IF NOT EXISTS pause_segments (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL REFERENCES work_orders(id) ON DELETE CASCADE,
    start_at      INTEGER NOT NULL,
    end_at        INTEGER,
    started_by    INTEGER NOT NULL REFERENCES users(id),
    ended_by      INTEGER REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_pause_segments_order ON pause_segments(work_order_id, start_at);

-- Older databases only have work segments. Recover pauses from the gaps
-- between them and from the gap before the recorded completion time.
INSERT INTO pause_segments(work_order_id, start_at, end_at, started_by, ended_by)
SELECT c.work_order_id, p.end_at, c.start_at,
       COALESCE(p.ended_by, p.started_by), c.started_by
  FROM time_segments c
  JOIN time_segments p ON p.work_order_id = c.work_order_id
 WHERE p.end_at IS NOT NULL
   AND c.start_at > p.end_at
   AND p.id = (
       SELECT p2.id FROM time_segments p2
        WHERE p2.work_order_id = c.work_order_id
          AND p2.end_at IS NOT NULL
          AND p2.end_at <= c.start_at
        ORDER BY p2.end_at DESC, p2.id DESC LIMIT 1
   )
   AND NOT EXISTS (
       SELECT 1 FROM pause_segments ps
        WHERE ps.work_order_id = c.work_order_id
          AND ps.start_at = p.end_at AND ps.end_at = c.start_at
   );

INSERT INTO pause_segments(work_order_id, start_at, end_at, started_by, ended_by)
SELECT t.work_order_id, t.end_at, o.completed_at,
       COALESCE(t.ended_by, t.started_by), COALESCE(t.ended_by, t.started_by)
  FROM time_segments t
  JOIN work_orders o ON o.id = t.work_order_id
 WHERE o.status = 'done'
   AND o.completed_at > t.end_at
   AND t.end_at = (
       SELECT MAX(t2.end_at) FROM time_segments t2
        WHERE t2.work_order_id = o.id AND t2.end_at IS NOT NULL
   )
   AND NOT EXISTS (
       SELECT 1 FROM pause_segments ps
        WHERE ps.work_order_id = o.id AND ps.start_at = t.end_at
   );

-- An order that was paused when the migration ran still has an open gap.
INSERT INTO pause_segments(work_order_id, start_at, started_by)
SELECT o.id, t.end_at, COALESCE(t.ended_by, t.started_by)
  FROM work_orders o
  JOIN time_segments t ON t.work_order_id = o.id
 WHERE o.timer_state = 'paused'
   AND t.end_at IS NOT NULL
   AND t.end_at = (
       SELECT MAX(t2.end_at) FROM time_segments t2
        WHERE t2.work_order_id = o.id AND t2.end_at IS NOT NULL
   )
   AND NOT EXISTS (
       SELECT 1 FROM pause_segments ps
        WHERE ps.work_order_id = o.id AND ps.end_at IS NULL
   );
