DROP INDEX IF EXISTS app.admin_broadcasts_due_idx;
ALTER TABLE app.admin_broadcasts
  DROP COLUMN IF EXISTS processed_at,
  DROP COLUMN IF EXISTS processing_started_at,
  DROP COLUMN IF EXISTS last_error,
  DROP COLUMN IF EXISTS recipient_count,
  DROP COLUMN IF EXISTS status;
ALTER TABLE app.notifications DROP COLUMN IF EXISTS in_app_enabled;
