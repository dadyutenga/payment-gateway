-- Migration: 000057_account_kind_backfill.down.sql
-- Removes the backfill scaffolding (review table + provenance column).
-- Kind flips applied by the up migration are DATA and intentionally stay
-- flipped: re-applying up afterwards re-derives the same kinds
-- deterministically. Resolve every app.account_kind_review row before
-- rolling back on a live database.

DROP TABLE IF EXISTS app.account_kind_review;

ALTER TABLE app.organizations
  DROP COLUMN IF EXISTS account_kind_source;
