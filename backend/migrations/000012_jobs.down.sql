BEGIN;
DO $$
DECLARE v integer;
BEGIN
 SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
 IF NOT FOUND OR v IS DISTINCT FROM 12 THEN RAISE EXCEPTION 'jobs rollback requires core schema version 12'; END IF;
END $$;
LOCK TABLE admin_jobs, admin_job_runs, security_events, menus, role_menus, role_permissions, permissions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM admin_jobs) OR EXISTS(SELECT 1 FROM admin_job_runs)
 OR EXISTS(SELECT 1 FROM security_events WHERE target_type='job')
 OR EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.code IN ('jobs:view','jobs:create','jobs:retry'))
 OR EXISTS(SELECT 1 FROM role_menus rm JOIN menus m ON m.id=rm.menu_id WHERE m.code='system_jobs')
 OR EXISTS(SELECT 1 FROM menus m JOIN permissions p ON p.id=m.permission_id WHERE p.code IN ('jobs:view','jobs:create','jobs:retry') AND m.code<>'system_jobs')
 OR EXISTS(SELECT 1 FROM menus child JOIN menus parent ON child.parent_id=parent.id WHERE parent.code='system_jobs')
 OR (SELECT count(*) FROM menus WHERE code='system_jobs')<>1
 OR EXISTS(SELECT 1 FROM menus m LEFT JOIN permissions p ON m.permission_id=p.id WHERE m.code='system_jobs' AND
 (m.name='Jobs' AND m.path='/admin/jobs' AND m.icon='ListTree' AND m.sort_order=82
 AND m.visible AND m.status='active' AND m.menu_type='page' AND m.component_key='jobs'
 AND m.is_system AND m.parent_id IS NULL AND p.code='jobs:view') IS NOT TRUE)
 OR EXISTS(SELECT 1 FROM (VALUES
 ('jobs:view','View jobs','Read background jobs and execution history'),
 ('jobs:create','Queue jobs','Queue an approved maintenance task'),
 ('jobs:retry','Retry jobs','Retry a failed maintenance task')
 ) seed(code,name,description) LEFT JOIN permissions p ON p.code=seed.code WHERE
 p.id IS NULL OR p.name IS DISTINCT FROM seed.name OR p.description IS DISTINCT FROM seed.description)
 THEN RAISE EXCEPTION 'jobs, grants, audit or seeds exist; refusing rollback'; END IF;
END $$;
DELETE FROM menus WHERE code='system_jobs';
DELETE FROM permissions WHERE code IN ('jobs:view','jobs:create','jobs:retry');
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
 CHECK (component_key IS NULL OR component_key IN
 ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications','api_keys'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
 CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification','api_key'));
DROP TABLE admin_job_runs;
DROP TABLE admin_jobs;
DO $$
DECLARE n integer;
BEGIN
 UPDATE app_metadata SET schema_version=11,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=12;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'jobs rollback version update failed'; END IF;
END $$;
COMMIT;
