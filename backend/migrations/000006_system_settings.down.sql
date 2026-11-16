BEGIN;
DELETE FROM security_events WHERE target_type='setting';
DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE component_key='settings');
DELETE FROM menus WHERE component_key='settings';
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('settings:view','settings:update'));
DELETE FROM permissions WHERE code IN ('settings:view','settings:update');
DROP TABLE system_settings;
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu'));
UPDATE app_metadata SET schema_version=5,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
