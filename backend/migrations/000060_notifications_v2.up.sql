-- Production notification model. The legacy dashboard table is upgraded in
-- place so existing in-app messages remain visible after the rollout.
ALTER TABLE app.dashboard_notifications RENAME TO notifications;
ALTER TABLE app.notifications RENAME COLUMN description TO body;
ALTER TABLE app.notifications RENAME COLUMN emoji TO icon;
ALTER TABLE app.notifications RENAME COLUMN link TO link_url;
ALTER TABLE app.notifications ADD COLUMN recipient_org_id UUID;
ALTER TABLE app.notifications ADD COLUMN recipient_user_id UUID;
ALTER TABLE app.notifications ADD COLUMN recipient_admin_id UUID;
ALTER TABLE app.notifications ADD COLUMN event_type TEXT;
ALTER TABLE app.notifications ADD COLUMN reference_id UUID;
ALTER TABLE app.notifications ADD COLUMN dedupe_key TEXT;
ALTER TABLE app.notifications ADD COLUMN severity TEXT NOT NULL DEFAULT 'info'
  CHECK (severity IN ('info', 'success', 'warning', 'alert'));
ALTER TABLE app.notifications ADD COLUMN source TEXT NOT NULL DEFAULT 'system'
  CHECK (source IN ('system', 'admin'));
ALTER TABLE app.notifications ADD COLUMN created_by_admin_id UUID;

UPDATE app.notifications
SET event_type = CASE WHEN audience = 'admin' THEN 'legacy.admin' ELSE 'account.welcome' END,
    recipient_user_id = CASE WHEN audience = 'customer' THEN recipient_id END,
    recipient_admin_id = CASE WHEN audience = 'admin' THEN recipient_id END,
    dedupe_key = 'legacy:' || id::text
WHERE event_type IS NULL;

ALTER TABLE app.notifications ALTER COLUMN event_type SET NOT NULL;
ALTER TABLE app.notifications ALTER COLUMN event_type SET DEFAULT 'system.general';
ALTER TABLE app.notifications DROP CONSTRAINT IF EXISTS dashboard_notifications_audience_check;
ALTER TABLE app.notifications DROP COLUMN recipient_id;
ALTER TABLE app.notifications DROP COLUMN audience;

ALTER TABLE app.notifications ADD CONSTRAINT notifications_recipient_check CHECK (
  (recipient_user_id IS NOT NULL AND recipient_admin_id IS NULL)
  OR (recipient_user_id IS NULL AND recipient_admin_id IS NOT NULL)
  OR (recipient_user_id IS NULL AND recipient_admin_id IS NULL AND recipient_org_id IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS notifications_recipient_dedupe_idx
  ON app.notifications (COALESCE(recipient_user_id, recipient_admin_id), dedupe_key)
  WHERE dedupe_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS notifications_org_unread_idx
  ON app.notifications (recipient_org_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS notifications_user_unread_idx
  ON app.notifications (recipient_user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS notifications_admin_unread_idx
  ON app.notifications (recipient_admin_id, read_at, created_at DESC);

-- Normalized preferences. Existing org-level switches are copied to all
-- channels and remain supported by the settings compatibility endpoint.
CREATE TABLE IF NOT EXISTS app.notification_preferences (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  scope_kind TEXT NOT NULL CHECK (scope_kind IN ('org', 'user', 'admin')),
  scope_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  channel TEXT NOT NULL CHECK (channel IN ('in_app', 'email', 'sms')),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (scope_kind, scope_id, event_type, channel)
);

INSERT INTO app.notification_preferences (scope_kind, scope_id, event_type, channel, enabled)
SELECT 'org', org_id, event_type, channel, enabled
FROM (
  SELECT org_id, 'payment.succeeded' AS event_type, payment_updated AS enabled FROM app.notification_prefs
  UNION ALL SELECT org_id, 'payment.failed', payment_updated FROM app.notification_prefs
  UNION ALL SELECT org_id, 'payment.refunded', payment_refunded FROM app.notification_prefs
  UNION ALL SELECT org_id, 'payment.expired', payment_expired FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.requested', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.requires_approval', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.approved', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.rejected', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.dispatched', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.completed', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'withdrawal.failed', withdrawal_updates FROM app.notification_prefs
  UNION ALL SELECT org_id, 'kyc.verified', kyc_decisions FROM app.notification_prefs
  UNION ALL SELECT org_id, 'kyc.rejected', kyc_decisions FROM app.notification_prefs
) legacy
CROSS JOIN (VALUES ('in_app'), ('email'), ('sms')) AS channels(channel)
ON CONFLICT (scope_kind, scope_id, event_type, channel) DO NOTHING;

CREATE INDEX IF NOT EXISTS notification_preferences_scope_idx
  ON app.notification_preferences (scope_kind, scope_id, event_type, channel);

CREATE TABLE IF NOT EXISTS app.notification_deliveries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  notification_id UUID NOT NULL REFERENCES app.notifications(id) ON DELETE CASCADE,
  channel TEXT NOT NULL CHECK (channel IN ('email', 'sms')),
  destination TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processing_started_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  sent_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (notification_id, channel, destination)
);
CREATE INDEX IF NOT EXISTS notification_deliveries_due_idx
  ON app.notification_deliveries (status, next_attempt_at);

CREATE TABLE IF NOT EXISTS app.admin_broadcasts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  icon TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'success', 'warning', 'alert')),
  target JSONB NOT NULL DEFAULT '{"kind":"all"}'::jsonb,
  scheduled_for TIMESTAMPTZ,
  created_by_admin_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS admin_broadcasts_created_idx
  ON app.admin_broadcasts (created_at DESC);
