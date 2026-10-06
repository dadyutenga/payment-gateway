-- Internal help desk tickets. This is deliberately separate from the public
-- creator/individual support-page (tip-jar) tables and routes.
CREATE TABLE IF NOT EXISTS app.support_tickets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES app.organizations(id) ON DELETE CASCADE,
  created_by UUID NOT NULL REFERENCES app.users(id) ON DELETE RESTRICT,
  subject TEXT NOT NULL,
  category TEXT NOT NULL CHECK (category IN ('payments', 'withdrawals', 'kyc', 'technical', 'billing', 'other')),
  priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'pending', 'in_progress', 'resolved', 'closed')),
  assigned_admin_id UUID REFERENCES app.admin_users(id) ON DELETE SET NULL,
  linked_order_id UUID REFERENCES app.payment_orders(id) ON DELETE SET NULL,
  linked_withdrawal_id UUID REFERENCES app.payment_withdrawals(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMPTZ,
  CHECK (linked_order_id IS NULL OR linked_withdrawal_id IS NULL)
);

CREATE TABLE IF NOT EXISTS app.support_messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  ticket_id UUID NOT NULL REFERENCES app.support_tickets(id) ON DELETE CASCADE,
  author_type TEXT NOT NULL CHECK (author_type IN ('merchant', 'admin')),
  author_id UUID NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  attachment_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
  internal_note BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS support_tickets_org_status_idx
  ON app.support_tickets (org_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS support_tickets_admin_queue_idx
  ON app.support_tickets (status, priority, created_at ASC);
CREATE INDEX IF NOT EXISTS support_tickets_assigned_idx
  ON app.support_tickets (assigned_admin_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS support_messages_ticket_idx
  ON app.support_messages (ticket_id, created_at ASC);

CREATE TRIGGER update_support_tickets_updated_at
  BEFORE UPDATE ON app.support_tickets
  FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
