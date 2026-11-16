BEGIN;

CREATE TABLE media_files (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_operation_id uuid NOT NULL UNIQUE,
    storage_key varchar(128) NOT NULL UNIQUE CHECK (storage_key ~ '^[a-f0-9]{32}$'),
    original_name varchar(255) NOT NULL CHECK (length(original_name) > 0),
    extension varchar(12) NOT NULL CHECK (extension IN ('png','jpg','jpeg','webp','pdf','txt')),
    mime_type varchar(100) NOT NULL CHECK (mime_type IN ('image/png','image/jpeg','image/webp','application/pdf','text/plain')),
    byte_size bigint NOT NULL CHECK (byte_size BETWEEN 1 AND 26214400),
    sha256 bytea NOT NULL CHECK (octet_length(sha256) = 32),
    width integer,
    height integer,
    uploaded_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
    visibility varchar(16) NOT NULL DEFAULT 'private' CHECK (visibility = 'private'),
    status varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','ready','deleting','delete_failed','deleted','failed','cleanup_failed','unavailable')),
    verified_generation uuid,
    reconcile_attempts integer NOT NULL DEFAULT 0 CHECK (reconcile_attempts BETWEEN 0 AND 5),
    next_reconcile_at timestamptz,
    last_error_code varchar(64),
    reconcile_claim_id uuid,
    reconcile_claim_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    deleted_at timestamptz,
    CONSTRAINT media_dimensions_check CHECK ((width IS NULL AND height IS NULL) OR (width BETWEEN 1 AND 100000 AND height BETWEEN 1 AND 100000)),
    CONSTRAINT media_type_size_check CHECK ((mime_type NOT LIKE 'image/%' OR byte_size <= 10485760)
        AND ((extension = 'png' AND mime_type = 'image/png') OR (extension IN ('jpg','jpeg') AND mime_type = 'image/jpeg')
          OR (extension = 'webp' AND mime_type = 'image/webp') OR (extension = 'pdf' AND mime_type = 'application/pdf')
          OR (extension = 'txt' AND mime_type = 'text/plain'))),
    CONSTRAINT media_claim_check CHECK ((reconcile_claim_id IS NULL) = (reconcile_claim_until IS NULL)),
    CONSTRAINT media_schedule_check CHECK (
        (status IN ('pending','deleting') AND next_reconcile_at IS NOT NULL AND reconcile_attempts < 5)
        OR (status = 'delete_failed' AND ((reconcile_attempts < 5 AND next_reconcile_at IS NOT NULL)
            OR (reconcile_attempts = 5 AND next_reconcile_at IS NULL)))
        OR (status IN ('ready','deleted','failed','cleanup_failed','unavailable') AND next_reconcile_at IS NULL))
);
CREATE INDEX media_ready_order_idx ON media_files(created_at DESC,id DESC) WHERE status='ready';
CREATE INDEX media_ready_type_idx ON media_files(mime_type,created_at DESC,id DESC) WHERE status='ready';
CREATE INDEX media_ready_uploader_idx ON media_files(uploaded_by,created_at DESC,id DESC) WHERE status='ready';
CREATE INDEX media_reconcile_idx ON media_files(status,next_reconcile_at,updated_at)
    WHERE status IN ('pending','deleting','delete_failed');

CREATE TABLE media_storage_binding (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    root_id uuid NOT NULL,
    active_verify_generation uuid,
    verification_state varchar(16) NOT NULL DEFAULT 'checking'
        CHECK (verification_state IN ('checking','complete','failed')),
    verified_at timestamptz,
    local_scan_cursors jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(local_scan_cursors) = 'object'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (verification_state <> 'complete' OR (active_verify_generation IS NOT NULL AND verified_at IS NOT NULL))
);

ALTER TABLE system_settings ADD COLUMN media_file_id uuid REFERENCES media_files(id) ON DELETE RESTRICT;
ALTER TABLE system_settings ADD CONSTRAINT system_settings_media_logo_check CHECK (
    media_file_id IS NULL OR (key = 'branding.logo_path' AND
        value_json = to_jsonb('/api/v1/admin/media/' || media_file_id::text || '/content'))
);
CREATE INDEX system_settings_media_file_idx ON system_settings(media_file_id) WHERE media_file_id IS NOT NULL;

ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu','setting','media'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus','settings','media'));
INSERT INTO permissions(code,name,description) VALUES
    ('media:view','View media','Browse and read private media'),
    ('media:upload','Upload media','Upload private media files'),
    ('media:delete','Delete media','Delete unreferenced private media')
ON CONFLICT(code) DO NOTHING;
UPDATE app_metadata SET schema_version=7,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
