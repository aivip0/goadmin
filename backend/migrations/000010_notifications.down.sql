BEGIN;
DO $$
DECLARE v integer;
BEGIN
  SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
  IF NOT FOUND OR v IS DISTINCT FROM 10 THEN RAISE EXCEPTION 'notifications rollback requires core schema version 10'; END IF;
END $$;
LOCK TABLE admin_notifications, security_events, menus, role_menus, role_permissions, permissions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM admin_notifications)
    OR EXISTS (SELECT 1 FROM security_events WHERE target_type='notification')
    OR EXISTS (SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.code IN ('notifications:view','notifications:create'))
    OR EXISTS (SELECT 1 FROM role_menus rm JOIN menus m ON m.id=rm.menu_id WHERE m.code='system_notifications')
    OR EXISTS (SELECT 1 FROM menus m JOIN permissions p ON p.id=m.permission_id WHERE p.code IN ('notifications:view','notifications:create') AND m.code<>'system_notifications')
    OR EXISTS (SELECT 1 FROM menus child JOIN menus parent ON parent.id=child.parent_id WHERE parent.code='system_notifications')
    OR (SELECT count(*) FROM menus WHERE code='system_notifications') <> 1
    OR EXISTS (SELECT 1 FROM menus m LEFT JOIN permissions p ON p.id=m.permission_id WHERE m.code='system_notifications'
       AND (m.name='Notifications' AND m.path='/admin/notifications' AND m.icon='Bell' AND m.sort_order=80
         AND m.visible AND m.status='active' AND m.menu_type='page' AND m.component_key='notifications'
         AND m.is_system AND m.parent_id IS NULL AND p.code='notifications:view') IS NOT TRUE)
    OR EXISTS (SELECT 1 FROM (VALUES
      ('notifications:view','View notifications','Read own notifications'),
      ('notifications:create','Send notifications','Send a notification to an administrator')
    ) seed(code,name,description) LEFT JOIN permissions p ON p.code=seed.code
       WHERE p.id IS NULL OR p.name IS DISTINCT FROM seed.name OR p.description IS DISTINCT FROM seed.description)
  THEN RAISE EXCEPTION 'notifications data, grants, audit or seed changed; refusing rollback'; END IF;
END $$;
DELETE FROM menus WHERE code='system_notifications';
DELETE FROM permissions WHERE code IN ('notifications:view','notifications:create');
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
  CHECK (component_key IS NULL OR component_key IN
    ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
  CHECK (target_type IN ('user','role','menu','setting','media','article','article_category'));
DROP TABLE admin_notifications;
DO $$
DECLARE n integer;
BEGIN
  UPDATE app_metadata SET schema_version=9,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=10;
  GET DIAGNOSTICS n = ROW_COUNT;
  IF n <> 1 THEN RAISE EXCEPTION 'notifications rollback version update failed'; END IF;
END $$;
COMMIT;
