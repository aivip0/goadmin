BEGIN;
LOCK TABLE login_events, security_events, menus, role_menus, role_permissions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM login_events)
       OR EXISTS (SELECT 1 FROM security_events WHERE request_method IS NOT NULL OR request_path IS NOT NULL OR client_ip IS NOT NULL OR user_agent IS NOT NULL OR outcome IS NOT NULL)
       OR EXISTS (SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.code IN ('audit:view','login_logs:view'))
       OR EXISTS (SELECT 1 FROM menus WHERE component_key IN ('audit_logs','login_logs')) THEN
        RAISE EXCEPTION 'log history or grants exist; refusing migration rollback';
    END IF;
END $$;
DROP TABLE login_events;
DROP INDEX security_events_action_time_idx;
DROP INDEX security_events_time_idx;
ALTER TABLE security_events DROP COLUMN request_method, DROP COLUMN request_path,
    DROP COLUMN client_ip, DROP COLUMN user_agent, DROP COLUMN outcome;
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN ('dashboard','users','roles','permissions','menus','settings','media'));
DELETE FROM permissions WHERE code IN ('audit:view','login_logs:view');
UPDATE app_metadata SET schema_version=7,updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
