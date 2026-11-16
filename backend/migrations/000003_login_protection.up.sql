BEGIN;
CREATE TABLE auth_login_limits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(key_hash) = 32),
    window_started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    failures INTEGER NOT NULL DEFAULT 0 CHECK (failures >= 0),
    blocked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX auth_login_limits_updated_idx ON auth_login_limits(updated_at);
UPDATE app_metadata SET schema_version=3, updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
