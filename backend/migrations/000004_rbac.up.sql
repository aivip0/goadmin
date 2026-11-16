BEGIN;
ALTER TABLE roles ADD COLUMN is_system boolean NOT NULL DEFAULT false;
UPDATE roles SET is_system=true, updated_at=CURRENT_TIMESTAMP WHERE is_super_admin;
INSERT INTO roles(code,name,description,is_system)
VALUES ('administrator','Administrator','System administration role; permissions are assigned explicitly',true)
ON CONFLICT(code) DO NOTHING;

INSERT INTO permissions(code,name,description) VALUES
('users:view','View administrators','Read administrator accounts'),
('users:create','Create administrators','Create administrator accounts'),
('users:update','Update administrators','Edit and enable or disable administrators'),
('users:delete','Delete administrators','Delete administrator accounts'),
('users:assign_roles','Assign administrator roles','Manage account role membership'),
('roles:view','View roles','Read role definitions and memberships'),
('roles:create','Create roles','Create custom roles'),
('roles:update','Update roles','Edit custom roles'),
('roles:delete','Delete roles','Delete unused custom roles'),
('roles:assign_permissions','Assign role permissions','Grant and revoke catalog permissions'),
('permissions:view','View permissions','Read the controlled permission catalog'),
('permissions:assign','Assign permissions','Manage role grants')
ON CONFLICT(code) DO NOTHING;

CREATE TABLE security_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid NOT NULL,
    action varchar(64) NOT NULL CHECK (action ~ '^[a-z_]+$'),
    target_type varchar(32) NOT NULL CHECK (target_type IN ('user','role')),
    target_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX security_events_target_idx ON security_events (target_type,target_id,created_at DESC);
CREATE INDEX security_events_actor_idx ON security_events (actor_user_id,created_at DESC);
UPDATE app_metadata SET schema_version=4,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
