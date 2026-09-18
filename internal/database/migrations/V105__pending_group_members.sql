-- Pre-provisioned group members: org admins can stage group memberships by
-- email before the person has an account. Rows are materialized into
-- group_members and consumed when the email first joins the org (registration,
-- SSO provisioning/auto-join, or invite redemption).
--
-- Org-scoped and case-insensitive: matching is always org_id + lower(email), so
-- a pending row can never pull a user into another org's group. No FK to users
-- -- the row exists precisely while the email has no account.
CREATE TABLE pending_group_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Expression unique constraints are not valid in a table constraint, so this
-- is a unique index instead. Also makes ON CONFLICT (group_id, lower(email))
-- inference work.
CREATE UNIQUE INDEX uq_pending_group_members_group_email ON pending_group_members (group_id, lower(email));
CREATE INDEX idx_pending_group_members_email ON pending_group_members (lower(email));
CREATE INDEX idx_pending_group_members_org ON pending_group_members (org_id);