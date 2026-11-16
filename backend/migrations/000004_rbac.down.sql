BEGIN;
DROP TABLE security_events;
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code IN (
'users:view','users:create','users:update','users:delete','users:assign_roles',
'roles:view','roles:create','roles:update','roles:delete','roles:assign_permissions',
'permissions:view','permissions:assign'));
DELETE FROM permissions WHERE code IN (
'users:view','users:create','users:update','users:delete','users:assign_roles',
'roles:view','roles:create','roles:update','roles:delete','roles:assign_permissions',
'permissions:view','permissions:assign');
DELETE FROM roles WHERE code='administrator' AND NOT EXISTS (SELECT 1 FROM user_roles WHERE role_id=roles.id);
ALTER TABLE roles DROP COLUMN is_system;
UPDATE app_metadata SET schema_version=3,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
