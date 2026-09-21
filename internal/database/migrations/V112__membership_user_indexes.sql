-- Membership lookups are user-scoped on the hot path (warehouse sync enqueue
-- resolution and permission checks). The composite primary keys lead with
-- group_id / org_id, so a bare user_id predicate cannot use them.
CREATE INDEX idx_group_members_user ON group_members(user_id);
CREATE INDEX idx_org_members_user ON org_members(user_id);
