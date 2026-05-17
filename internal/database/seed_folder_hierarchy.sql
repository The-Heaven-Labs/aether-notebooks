-- Seed data for Hell org to demonstrate folder hierarchy and permissions
-- Org: Hell (3bbabfed-3c4a-4615-9bc2-5b43a21e665d)
-- Users: Demon (0c07ecfd-9cd9-4862-934c-494cec1c0c84), Angel (94cc2e7c-7cae-4e25-afe4-575e81c6e996)

BEGIN;

-- Create Angel's Home folder (with is_home = true)
INSERT INTO folders (id, org_id, parent_id, name, is_home, owner_id, created_by)
VALUES ('a0000000-0000-0000-0000-000000000005', '3bbabfed-3c4a-4615-9bc2-5b43a21e665d', NULL, 'Angel Home', true, '94cc2e7c-7cae-4e25-afe4-575e81c6e996', '94cc2e7c-7cae-4e25-afe4-575e81c6e996');

-- ACL for Angel's home folder (owner gets full permissions via is_home ACL seed in migration 010)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES ('3bbabfed-3c4a-4615-9bc2-5b43a21e665d', 'folder', 'a0000000-0000-0000-0000-000000000005', 'user', '94cc2e7c-7cae-4e25-afe4-575e81c6e996', ARRAY['view','create','edit','manage','delete']);

-- Create "Shared Projects" root folder (both users can access)
INSERT INTO folders (id, org_id, parent_id, name, is_home, owner_id, created_by)
VALUES ('a0000000-0000-0000-0000-000000000001', '3bbabfed-3c4a-4615-9bc2-5b43a21e665d', NULL, 'Shared Projects', false, '0c07ecfd-9cd9-4862-934c-494cec1c0c84', '0c07ecfd-9cd9-4862-934c-494cec1c0c84');

-- Create "Analytics" subfolder under Shared Projects
INSERT INTO folders (id, org_id, parent_id, name, is_home, owner_id, created_by)
VALUES ('a0000000-0000-0000-0000-000000000002', '3bbabfed-3c4a-4615-9bc2-5b43a21e665d', 'a0000000-0000-0000-0000-000000000001', 'Analytics', false, '0c07ecfd-9cd9-4862-934c-494cec1c0c84', '0c07ecfd-9cd9-4862-934c-494cec1c0c84');

-- Create "Engineering" subfolder under Shared Projects
INSERT INTO folders (id, org_id, parent_id, name, is_home, owner_id, created_by)
VALUES ('a0000000-0000-0000-0000-000000000003', '3bbabfed-3c4a-4615-9bc2-5b43a21e665d', 'a0000000-0000-0000-0000-000000000001', 'Engineering', false, '0c07ecfd-9cd9-4862-934c-494cec1c0c84', '0c07ecfd-9cd9-4862-934c-494cec1c0c84');

-- Create "ML Research" subfolder under Angel's Home
INSERT INTO folders (id, org_id, parent_id, name, is_home, owner_id, created_by)
VALUES ('a0000000-0000-0000-0000-000000000004', '3bbabfed-3c4a-4615-9bc2-5b43a21e665d', 'a0000000-0000-0000-0000-000000000005', 'ML Research', false, '94cc2e7c-7cae-4e25-afe4-575e81c6e996', '94cc2e7c-7cae-4e25-afe4-575e81c6e996');

-- Create "Model Training" notebook in ML Research folder
INSERT INTO notebooks (id, org_id, title, description, connector_id, parameters, created_by, folder_id)
VALUES (
  'b0000000-0000-0000-0000-000000000003',
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'Model Training',
  'Training scripts for recommendation model',
  NULL,
  '[]',
  '94cc2e7c-7cae-4e25-afe4-575e81c6e996',
  'a0000000-0000-0000-0000-000000000004'
);

-- Create "Personal Notes" notebook in Angel's Home folder
INSERT INTO notebooks (id, org_id, title, description, connector_id, parameters, created_by, folder_id)
VALUES (
  'b0000000-0000-0000-0000-000000000004',
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'Personal Notes',
  'My private notes and ideas',
  NULL,
  '[]',
  '94cc2e7c-7cae-4e25-afe4-575e81c6e996',
  'a0000000-0000-0000-0000-000000000005'
);

-- Set up ACLs for Shared Projects folder
-- Give Angel view+edit on Shared Projects (she's not owner, so needs explicit ACL)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'folder',
  'a0000000-0000-0000-0000-000000000001',
  'user',
  '94cc2e7c-7cae-4e25-afe4-575e81c6e996',
  ARRAY['view', 'create', 'edit']
);

-- Give Angel view-only on Analytics (deeper inheritance)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'folder',
  'a0000000-0000-0000-0000-000000000002',
  'user',
  '94cc2e7c-7cae-4e25-afe4-575e81c6e996',
  ARRAY['view']
);

-- Give Angel view on Engineering notebooks
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'notebook',
  'b0000000-0000-0000-0000-000000000002',
  'user',
  '94cc2e7c-7cae-4e25-afe4-575e81c6e996',
  ARRAY['view', 'run']
);

-- Give Demon view+edit on ML Research (Angel's private folder - for demo purposes)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'folder',
  'a0000000-0000-0000-0000-000000000004',
  'user',
  '0c07ecfd-9cd9-4862-934c-494cec1c0c84',
  ARRAY['view', 'create']
);

-- Give Demon view on Personal Notes
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'notebook',
  'b0000000-0000-0000-0000-000000000004',
  'user',
  '0c07ecfd-9cd9-4862-934c-494cec1c0c84',
  ARRAY['view']
);

-- Create a "Data Team" group and add both users
INSERT INTO groups (id, org_id, name)
VALUES ('c0000000-0000-0000-0000-000000000001', '3bbabfed-3c4a-4615-9bc2-5b43a21e665d', 'Data Team');

INSERT INTO group_members (group_id, user_id)
VALUES ('c0000000-0000-0000-0000-000000000001', '0c07ecfd-9cd9-4862-934c-494cec1c0c84');

INSERT INTO group_members (group_id, user_id)
VALUES ('c0000000-0000-0000-0000-000000000001', '94cc2e7c-7cae-4e25-afe4-575e81c6e996');

-- Give Data Team view+create on Shared Projects (group-level ACL)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'folder',
  'a0000000-0000-0000-0000-000000000001',
  'group',
  'c0000000-0000-0000-0000-000000000001',
  ARRAY['view', 'create']
);

-- Give Data Team view+edit on Sales Dashboard notebook
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
VALUES (
  '3bbabfed-3c4a-4615-9bc2-5b43a21e665d',
  'notebook',
  'b0000000-0000-0000-0000-000000000001',
  'group',
  'c0000000-0000-0000-0000-000000000001',
  ARRAY['view', 'edit', 'run']
);

COMMIT;