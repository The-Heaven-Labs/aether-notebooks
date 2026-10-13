-- Builtin tool seed revision: seeding upserts description/schema/config when a
-- builtin row's seed_rev is older than the catalog entry's Rev, so changed
-- descriptions reach existing orgs (previously ON CONFLICT DO NOTHING plus a
-- count check meant changed text never refreshed). Builtin descriptions are
-- platform-managed; a user edit is overwritten on the next revision bump.
ALTER TABLE tools ADD COLUMN seed_rev INT NOT NULL DEFAULT 0;
