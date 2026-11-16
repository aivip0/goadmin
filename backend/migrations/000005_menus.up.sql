BEGIN;

ALTER TABLE menus
    ADD COLUMN menu_type varchar(16) NOT NULL DEFAULT 'page'
        CHECK (menu_type IN ('directory','page','external')),
    ADD COLUMN component_key varchar(32),
    ADD COLUMN is_system boolean NOT NULL DEFAULT false;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus'));
CREATE INDEX menus_type_status_idx ON menus(menu_type,status,visible);

ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu'));

INSERT INTO permissions(code,name,description) VALUES
('dashboard:view','View dashboard','Open the administration home'),
('menus:view','View menus','Read menu definitions'),
('menus:create','Create menus','Create menu definitions'),
('menus:update','Update menus','Edit menu definitions and ordering'),
('menus:delete','Delete menus','Delete menu definitions'),
('menus:assign_roles','Assign menus to roles','Manage role menu visibility')
ON CONFLICT(code) DO NOTHING;

UPDATE app_metadata SET schema_version=5,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
