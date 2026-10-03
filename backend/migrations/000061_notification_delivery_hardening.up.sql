-- Keep the notification row as the durable event record while allowing the
-- in-app channel to be muted independently from email/SMS delivery.
ALTER TABLE app.notifications
  ADD COLUMN IF NOT EXISTS in_app_enabled BOOLEAN NOT NULL DEFAULT TRUE;

-- Broadcast creation is intentionally quick. Fan-out is claimed and handled
-- by the API/worker background loop so a large audience never blocks admin's
-- request.
ALTER TABLE app.admin_broadcasts
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued', 'processing', 'sent', 'failed')),
  ADD COLUMN IF NOT EXISTS recipient_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS processing_started_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS admin_broadcasts_due_idx
  ON app.admin_broadcasts (status, scheduled_for, created_at);
