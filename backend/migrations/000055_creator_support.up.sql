-- Migration: 000055_creator_support.up.sql
-- Public "support me" pages (Part 4): per-creator support settings plus
-- the labeled amount buttons shown on the page. This is the smallest
-- link model that unblocks support payments before Block 3 lands — when
-- Block 3's payment_links table merges, these rows map onto it
-- (label + fixed/open amount are the shared shape) rather than living
-- beside it forever.
--
-- No second money path: support orders are created through the single
-- payments.Service.CreateOrder call; these tables only configure the
-- public page (bounds, buttons, receiving app).

CREATE TABLE IF NOT EXISTS app.creator_support_settings (
  org_id UUID PRIMARY KEY REFERENCES app.organizations(id) ON DELETE CASCADE,
  support_app_id UUID REFERENCES app.payment_apps(id) ON DELETE SET NULL,
  min_amount TEXT,
  max_amount TEXT,
  -- Supporters-wall toggle: future scope, default OFF. Stored so the
  -- default is explicit; never rendered publicly until the toggle ships.
  show_supporters_wall BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_creator_support_settings_updated_at
  BEFORE UPDATE ON app.creator_support_settings
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE IF NOT EXISTS app.creator_support_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES app.organizations(id) ON DELETE CASCADE,
  label TEXT NOT NULL DEFAULT '',
  amount_mode TEXT NOT NULL DEFAULT 'open'
    CHECK (amount_mode IN ('fixed', 'open')),
  amount TEXT,
  sort_order INTEGER NOT NULL DEFAULT 0,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS creator_support_links_org_idx
  ON app.creator_support_links (org_id, sort_order ASC);
