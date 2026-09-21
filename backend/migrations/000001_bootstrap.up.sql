-- Migration: 000001_bootstrap.up.sql
-- Two things every later payments migration assumes already exist:
--
-- 1. update_updated_at_column() — a generic trigger function the payments
--    tables attach to their updated_at column. In the full AZSUBAY
--    backend this lives in a much larger init migration alongside
--    unrelated tables (products, pages, ...); this package only needs
--    the function itself.
--
-- 2. app.users — local credentials and roles owned by this service.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE SCHEMA IF NOT EXISTS app;
CREATE TABLE IF NOT EXISTS app.users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  full_name TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  is_admin BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_idx ON app.users (lower(email));
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON app.users FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
