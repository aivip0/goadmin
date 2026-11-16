BEGIN;

ALTER TABLE security_events
    ADD COLUMN request_method varchar(8) CHECK (request_method IN ('GET','POST','PUT','PATCH','DELETE')),
    ADD COLUMN request_path varchar(200) CHECK (request_path LIKE '/api/v1/%' AND request_path NOT LIKE '%?%' AND request_path NOT LIKE '%#%'),
    ADD COLUMN client_ip inet,
    ADD COLUMN user_agent varchar(256),
    ADD COLUMN outcome varchar(16) CHECK (outcome IN ('success','failure'));
CREATE INDEX security_events_time_idx ON security_events(created_at DESC,id DESC);
CREATE INDEX security_events_action_time_idx ON security_events(action,created_at DESC,id DESC);

CREATE TABLE login_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid REFERENCES admin_users(id) ON DELETE SET NULL,
    account_kind varchar(16) NOT NULL CHECK (account_kind IN ('known','unknown')),
    outcome varchar(20) NOT NULL CHECK (outcome IN ('success','failure','rate_limited')),
    client_ip inet,
    user_agent varchar(256),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT login_events_account_check CHECK (account_kind <> 'unknown' OR user_id IS NULL)
);
CREATE INDEX login_events_time_idx ON login_events(created_at DESC,id DESC);
CREATE INDEX login_events_user_time_idx ON login_events(user_id,created_at DESC,id DESC) WHERE user_id IS NOT NULL;
CREATE INDEX login_events_outcome_time_idx ON login_events(outcome,created_at DESC,id DESC);

ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs'));
INSERT INTO permissions(code,name,description) VALUES
    ('audit:view','View audit logs','Read administrator operation history'),
    ('login_logs:view','View login logs','Read classified authentication outcomes')
ON CONFLICT(code) DO NOTHING;
UPDATE app_metadata SET schema_version=8,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
