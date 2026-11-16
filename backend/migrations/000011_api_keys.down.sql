BEGIN;
DO $$
DECLARE v integer;
BEGIN
 SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
 IF NOT FOUND OR v IS DISTINCT FROM 11 THEN RAISE EXCEPTION 'API key rollback requires core schema version 11'; END IF;
END $$;
LOCK TABLE admin_api_keys, security_events, menus, role_menus, role_permissions, permissions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM admin_api_keys)
 OR EXISTS(SELECT 1 FROM security_events WHERE target_type='api_key')
 OR EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.code IN ('api_keys:view','api_keys:create','api_keys:revoke'))
 OR EXISTS(SELECT 1 FROM role_menus rm JOIN menus m ON m.id=rm.menu_id WHERE m.code='system_api_keys')
 OR EXISTS(SELECT 1 FROM menus m JOIN permissions p ON p.id=m.permission_id WHERE p.code IN ('api_keys:view','api_keys:create','api_keys:revoke') AND m.code<>'system_api_keys')
 OR EXISTS(SELECT 1 FROM menus child JOIN menus parent ON child.parent_id=parent.id WHERE parent.code='system_api_keys')
 OR (SELECT count(*) FROM menus WHERE code='system_api_keys')<>1
 OR EXISTS(SELECT 1 FROM menus m LEFT JOIN permissions p ON m.permission_id=p.id WHERE m.code='system_api_keys' AND
 (m.name='API Keys' AND m.path='/admin/api-keys' AND m.icon='KeyRound' AND m.sort_order=81
 AND m.visible AND m.status='active' AND m.menu_type='page' AND m.component_key='api_keys'
 AND m.is_system AND m.parent_id IS NULL AND p.code='api_keys:view') IS NOT TRUE)
 OR EXISTS(SELECT 1 FROM (VALUES
 ('api_keys:view','View API keys','List own API keys'),
 ('api_keys:create','Create API keys','Issue a scoped API key'),
 ('api_keys:revoke','Revoke API keys','Revoke own API keys')
 ) seed(code,name,description) LEFT JOIN permissions p ON p.code=seed.code WHERE
 p.id IS NULL OR p.name IS DISTINCT FROM seed.name OR p.description IS DISTINCT FROM seed.description)
 THEN RAISE EXCEPTION 'API keys, grants, audit or seeds exist; refusing rollback'; END IF;
END $$;
DELETE FROM menus WHERE code='system_api_keys';
DELETE FROM permissions WHERE code IN ('api_keys:view','api_keys:create','api_keys:revoke');
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
 CHECK (component_key IS NULL OR component_key IN
 ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
 CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification'));
DROP TABLE admin_api_keys;
DO $$
DECLARE n integer;
BEGIN
 UPDATE app_metadata SET schema_version=10,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=11;
 GET DIAGNOSTICS n = ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'API key rollback version update failed'; END IF;
END $$;
COMMIT;
