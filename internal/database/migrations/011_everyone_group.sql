-- Seed "Everyone" group for all existing orgs that don't have one yet
INSERT INTO groups (org_id, name)
SELECT id, 'Everyone'
FROM orgs
ON CONFLICT (org_id, name) DO NOTHING;
