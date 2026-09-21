-- Migration: 000024_payment_app_members.up.sql
-- Links a service-managed gateway user
-- users to specific payment_apps, so a future merchant-facing dashboard can
-- scope data to "the app(s) I belong to" instead of admin-wide access.
-- No FK to auth.users (matches the existing app.admin_activity_log
-- convention) and no role column — there's no permission differentiation
-- to enforce yet.

CREATE TABLE IF NOT EXISTS app.payment_app_members (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  user_id UUID NOT NULL,
  added_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (app_id, user_id)
);

CREATE INDEX IF NOT EXISTS payment_app_members_app_idx ON app.payment_app_members (app_id);
CREATE INDEX IF NOT EXISTS payment_app_members_user_idx ON app.payment_app_members (user_id);
