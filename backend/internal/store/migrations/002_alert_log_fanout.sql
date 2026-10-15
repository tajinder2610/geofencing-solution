-- Delivery is now fanned out via NOTIFY to every instance, so a per-instance client count is meaningless.
ALTER TABLE alert_log DROP COLUMN IF EXISTS clients_notified;
