-- SPDX-License-Identifier: AGPL-3.0-only

-- The Soll is now an explicit value on the work order. Keep the Soll of
-- existing orders by copying their former task-minute sum into that column.
UPDATE work_orders
   SET planned_minutes = COALESCE((
       SELECT SUM(planned_minutes) FROM tasks WHERE work_order_id = work_orders.id
   ), 0)
 WHERE planned_minutes = 0;
