BEGIN;
LOCK TABLE media_files, system_settings, security_events, menus, role_menus, role_permissions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM media_files)
       OR EXISTS (SELECT 1 FROM media_storage_binding)
       OR EXISTS (SELECT 1 FROM system_settings WHERE media_file_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM security_events WHERE target_type='media')
       OR EXISTS (SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.code LIKE 'media:%')
       OR EXISTS (SELECT 1 FROM role_menus rm JOIN menus m ON m.id=rm.menu_id WHERE m.component_key='media')
       OR EXISTS (SELECT 1 FROM menus WHERE component_key='media') THEN
        RAISE EXCEPTION 'media history or grants exist; refusing migration rollback';
    END IF;
END $$;
-- An operator must separately prove that the bound private root is empty before using Down.
ALTER TABLE system_settings DROP CONSTRAINT system_settings_media_logo_check;
ALTER TABLE system_settings DROP COLUMN media_file_id;
DROP TABLE media_storage_binding;
DROP TABLE media_files;
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus','settings'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu','setting'));
DELETE FROM permissions WHERE code IN ('media:view','media:upload','media:delete');
UPDATE app_metadata SET schema_version=6,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
