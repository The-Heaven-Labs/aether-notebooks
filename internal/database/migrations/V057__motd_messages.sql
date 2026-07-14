CREATE TABLE IF NOT EXISTS motd_messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id),
  title TEXT,
  content TEXT NOT NULL,
  priority INT DEFAULT 0,
  visibility TEXT NOT NULL DEFAULT 'all',
  pages TEXT[],
  show_on_login BOOLEAN DEFAULT false,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_motd_messages_org_id ON motd_messages(org_id);
