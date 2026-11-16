CREATE TABLE app_metadata (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    component VARCHAR(64) NOT NULL UNIQUE,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO app_metadata (component, schema_version) VALUES ('core', 1);
