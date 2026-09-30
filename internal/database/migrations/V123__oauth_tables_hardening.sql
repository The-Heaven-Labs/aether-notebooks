-- Migration 123: hardening for the OAuth AS tables (V122).
-- Adds referential integrity for client/token references, pins PKCE to S256,
-- and indexes the expiry columns used by cleanup/retention queries.

ALTER TABLE oauth_auth_codes
    ADD CONSTRAINT oauth_auth_codes_client_fk
    FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    ADD CONSTRAINT oauth_auth_codes_challenge_method_s256
    CHECK (challenge_method = 'S256');

ALTER TABLE oauth_tokens
    ADD CONSTRAINT oauth_tokens_client_fk
    FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    ADD CONSTRAINT oauth_tokens_replaced_by_fk
    FOREIGN KEY (replaced_by) REFERENCES oauth_tokens(id) ON DELETE SET NULL;

CREATE INDEX idx_oauth_auth_codes_client ON oauth_auth_codes (client_id);
CREATE INDEX idx_oauth_tokens_client ON oauth_tokens (client_id);
CREATE INDEX idx_oauth_auth_codes_expires ON oauth_auth_codes (expires_at);
CREATE INDEX idx_oauth_tokens_expires ON oauth_tokens (expires_at);
