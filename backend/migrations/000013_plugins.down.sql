BEGIN;
DO $$
DECLARE v integer;
BEGIN
 SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
 IF NOT FOUND OR v IS DISTINCT FROM 13 THEN RAISE EXCEPTION 'plugins rollback requires core schema version 13'; END IF;
END $$;
LOCK TABLE admin_plugins, admin_plugin_config, admin_plugin_migrations, admin_plugin_events,
 security_events, menus, role_menus, role_permissions, permissions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM admin_plugins) OR EXISTS(SELECT 1 FROM admin_plugin_config)
 OR EXISTS(SELECT 1 FROM admin_plugin_migrations) OR EXISTS(SELECT 1 FROM admin_plugin_events)
 OR EXISTS(SELECT 1 FROM security_events WHERE target_type='plugin')
 OR EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.code LIKE 'plugins:%')
 OR EXISTS(SELECT 1 FROM role_menus rm JOIN menus m ON m.id=rm.menu_id WHERE m.code='system_plugins')
 OR EXISTS(SELECT 1 FROM menus m JOIN permissions p ON p.id=m.permission_id WHERE p.code LIKE 'plugins:%' AND m.code<>'system_plugins')
 OR EXISTS(SELECT 1 FROM menus child JOIN menus parent ON child.parent_id=parent.id WHERE parent.code='system_plugins')
 OR (SELECT count(*) FROM menus WHERE code='system_plugins')<>1
 OR EXISTS(SELECT 1 FROM menus m LEFT JOIN permissions p ON p.id=m.permission_id WHERE m.code='system_plugins' AND
   (m.name='Plugins' AND m.path='/admin/plugins' AND m.icon='Puzzle' AND m.sort_order=86
    AND m.visible AND m.status='active' AND m.menu_type='page' AND m.component_key='plugins'
    AND m.is_system AND m.parent_id IS NULL AND p.code='plugins:view') IS NOT TRUE)
 OR EXISTS(SELECT 1 FROM (VALUES
  ('plugins:view','View plugins','Read installed plugin metadata and event history'),
  ('plugins:install','Install plugins','Install a trusted local plugin package'),
  ('plugins:enable','Enable plugins','Enable a registered plugin and its routes'),
  ('plugins:disable','Disable plugins','Disable a registered plugin and its routes'),
  ('plugins:uninstall','Uninstall plugins','Remove an installed plugin while retaining event history'),
  ('plugins:configure','Configure plugins','Update plugin configuration'),
  ('plugins:migrate','Migrate plugins','Apply signed plugin database migrations')
 ) seed(code,name,description) LEFT JOIN permissions p ON p.code=seed.code
 WHERE p.id IS NULL OR p.name IS DISTINCT FROM seed.name OR p.description IS DISTINCT FROM seed.description)
 THEN RAISE EXCEPTION 'plugin data, grants, audit or seeds exist; refusing rollback'; END IF;
END $$;
DELETE FROM menus WHERE code='system_plugins';
DELETE FROM permissions WHERE code LIKE 'plugins:%';
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
 CHECK (component_key IS NULL OR component_key IN
 ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications','api_keys','jobs'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
 CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification','api_key','job'));
DROP TABLE admin_plugin_events;
DROP TABLE admin_plugin_migrations;
DROP TABLE admin_plugin_config;
DROP TABLE admin_plugins;
DO $$
DECLARE n integer;
BEGIN
 UPDATE app_metadata SET schema_version=12,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=13;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'plugins rollback version update failed'; END IF;
END $$;
COMMIT;
