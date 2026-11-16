BEGIN;
CREATE TABLE system_settings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key varchar(128) NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_.]+$'),
    value_json jsonb,
    is_sensitive boolean NOT NULL DEFAULT false,
    value_ciphertext bytea,
    encryption_key_version integer,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT system_settings_storage_check CHECK (
        (NOT is_sensitive AND value_json IS NOT NULL AND value_ciphertext IS NULL AND encryption_key_version IS NULL)
        OR (is_sensitive AND value_json IS NULL AND value_ciphertext IS NOT NULL AND encryption_key_version > 0)
    )
);
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu','setting'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus','settings'));
INSERT INTO permissions(code,name,description) VALUES
    ('settings:view','View system settings','Read allowlisted system settings'),
    ('settings:update','Update system settings','Edit allowlisted system settings')
ON CONFLICT(code) DO NOTHING;
UPDATE app_metadata SET schema_version=6,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
