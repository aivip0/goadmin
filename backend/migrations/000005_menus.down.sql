BEGIN;
UPDATE menus SET permission_id=NULL WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('dashboard:view','menus:view','menus:create','menus:update','menus:delete','menus:assign_roles')
);
DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE is_system);
DELETE FROM menus WHERE is_system;
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('dashboard:view','menus:view','menus:create','menus:update','menus:delete','menus:assign_roles')
);
DELETE FROM permissions WHERE code IN ('dashboard:view','menus:view','menus:create','menus:update','menus:delete','menus:assign_roles');
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check CHECK (target_type IN ('user','role'));
DROP INDEX menus_type_status_idx;
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus DROP COLUMN is_system,DROP COLUMN component_key,DROP COLUMN menu_type;
UPDATE app_metadata SET schema_version=4,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
