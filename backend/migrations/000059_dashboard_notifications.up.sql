-- In-app dashboard notifications shared by customer workspaces and operators.
-- The audience column keeps customer and admin identity namespaces separate.
CREATE TABLE IF NOT EXISTS app.dashboard_notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  recipient_id UUID NOT NULL,
  audience TEXT NOT NULL CHECK (audience IN ('customer', 'admin')),
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  emoji TEXT NOT NULL DEFAULT '',
  link TEXT NOT NULL DEFAULT '',
  read_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS dashboard_notifications_recipient_idx
  ON app.dashboard_notifications (recipient_id, audience, created_at DESC);
