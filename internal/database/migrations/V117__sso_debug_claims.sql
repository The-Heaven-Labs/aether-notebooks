-- Opt-in capture of the redacted IDP payloads observed during an OIDC login,
-- for troubleshooting group-claim mapping. Off by default; captures live in
-- Redis with a short TTL (latest login wins).
ALTER TABLE sso_providers
  ADD COLUMN debug_claims bool NOT NULL DEFAULT false;
