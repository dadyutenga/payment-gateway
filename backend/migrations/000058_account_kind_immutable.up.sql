-- Account kind is selected by the track-specific server endpoint and cannot
-- be changed by application updates. Support must create a new account;
-- there is no self-service conversion between tracks.
CREATE OR REPLACE FUNCTION app.prevent_account_kind_change()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.account_kind IS DISTINCT FROM OLD.account_kind THEN
    RAISE EXCEPTION 'account_kind is immutable after creation'
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS organizations_account_kind_immutable ON app.organizations;
CREATE TRIGGER organizations_account_kind_immutable
  BEFORE UPDATE OF account_kind ON app.organizations
  FOR EACH ROW
  EXECUTE FUNCTION app.prevent_account_kind_change();
