-- Indexed keyed-hash lookup for personal access tokens.
--
-- Tokens are high-entropy random values, so authentication can look them up by
-- an HMAC-SHA-256 of the raw token instead of bcrypt-scanning every row.
-- The bcrypt token_hash column is retained for tokens created before this
-- migration; those rows are backfilled with the lookup hash on first use.
ALTER TABLE api_tokens ADD COLUMN IF NOT EXISTS token_lookup_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_token_lookup_hash
  ON api_tokens(token_lookup_hash)
  WHERE token_lookup_hash IS NOT NULL;
