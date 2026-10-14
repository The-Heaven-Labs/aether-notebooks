-- Migration 134: custom headers for model configs and MCP servers.
-- Sensitive header values are stored as enc:v1:-tagged AES-GCM ciphertext
-- (internal/crypto.EncryptString); the *_secrets columns list which header
-- names are sensitive. Webhook tools keep tools.config.headers/secrets and
-- upgrade to ciphertext lazily on their next save.
ALTER TABLE model_configs
    ADD COLUMN custom_headers JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN custom_header_secrets TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE mcp_servers
    ADD COLUMN headers JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN header_secrets TEXT[] NOT NULL DEFAULT '{}';
