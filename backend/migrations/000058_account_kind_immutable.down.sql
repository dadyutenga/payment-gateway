DROP TRIGGER IF EXISTS organizations_account_kind_immutable ON app.organizations;
DROP FUNCTION IF EXISTS app.prevent_account_kind_change();
