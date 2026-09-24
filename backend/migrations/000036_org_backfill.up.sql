-- Migration: 000036_org_backfill.up.sql
-- Moves payment_app_members into the org model (the old table is left
-- untouched — re-running up is safe, and down-migration documents the
-- tradeoffs):
--   * one org per distinct app owner (earliest self-added member per app,
--     else earliest member); slug embeds the full owner UUID, unique by
--     construction.
--   * memberless apps share a single 'unclaimed' org.
--   * EVERY legacy member becomes org owner: members have full merchant
--     access today, so any narrower default would silently revoke actions
--     they currently perform. Tighten roles afterwards via the API.
--   * apps.org_id added nullable here and backfilled; 000037 enforces
--     NOT NULL + FK once data is known good.

-- One org per distinct owner.
INSERT INTO app.organizations (name, slug)
SELECT DISTINCT
  COALESCE(NULLIF(split_part(u.email, '@', 1), ''), 'org') || '''s organization',
  'org-' || replace(o.owner_id::text, '-', '')
FROM (
  SELECT DISTINCT owner_id FROM (
    SELECT m.app_id, m.user_id AS owner_id,
      ROW_NUMBER() OVER (
        PARTITION BY m.app_id
        ORDER BY (m.added_by = m.user_id::text) DESC, m.created_at ASC
      ) AS rn
    FROM app.payment_app_members m
  ) ranked WHERE rn = 1
) o
LEFT JOIN app.users u ON u.id = o.owner_id
ON CONFLICT (slug) DO NOTHING;

-- Shared org for apps with no members at all.
INSERT INTO app.organizations (name, slug)
VALUES ('Unclaimed', 'unclaimed')
ON CONFLICT (slug) DO NOTHING;

-- Every legacy membership becomes an active owner row in its app's org.
INSERT INTO app.org_members (org_id, user_id, role, invited_by, status)
SELECT org.id, m.user_id, 'owner', m.added_by, 'active'
FROM app.payment_app_members m
JOIN (
  SELECT m2.app_id, m2.user_id AS owner_id FROM (
    SELECT m3.app_id, m3.user_id,
      ROW_NUMBER() OVER (
        PARTITION BY m3.app_id
        ORDER BY (m3.added_by = m3.user_id::text) DESC, m3.created_at ASC
      ) AS rn
    FROM app.payment_app_members m3
  ) m2 WHERE m2.rn = 1
) ao ON ao.app_id = m.app_id
JOIN app.organizations org ON org.slug = 'org-' || replace(ao.owner_id::text, '-', '')
ON CONFLICT (org_id, user_id) DO NOTHING;

-- Nullable org linkage on apps, then backfill.
ALTER TABLE app.payment_apps ADD COLUMN IF NOT EXISTS org_id UUID;

UPDATE app.payment_apps a SET org_id = org.id
FROM (
  SELECT m2.app_id, m2.user_id AS owner_id FROM (
    SELECT m3.app_id, m3.user_id,
      ROW_NUMBER() OVER (
        PARTITION BY m3.app_id
        ORDER BY (m3.added_by = m3.user_id::text) DESC, m3.created_at ASC
      ) AS rn
    FROM app.payment_app_members m3
  ) m2 WHERE m2.rn = 1
) ao
JOIN app.organizations org ON org.slug = 'org-' || replace(ao.owner_id::text, '-', '')
WHERE a.id = ao.app_id AND a.org_id IS NULL;

UPDATE app.payment_apps a SET org_id = org.id
FROM app.organizations org
WHERE org.slug = 'unclaimed' AND a.org_id IS NULL;
