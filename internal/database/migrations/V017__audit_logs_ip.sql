-- Add ip column to audit_logs for client IP tracking
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS ip TEXT;
CREATE INDEX IF NOT EXISTS idx_audit_logs_ip ON audit_logs (ip);