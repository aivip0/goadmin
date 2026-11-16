BEGIN;
DO $$
DECLARE v integer;
BEGIN
 SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
 IF NOT FOUND OR v IS DISTINCT FROM 11 THEN RAISE EXCEPTION 'jobs require core schema version 11'; END IF;
 IF EXISTS (SELECT 1 FROM permissions WHERE code IN ('jobs:view','jobs:create','jobs:retry'))
 OR EXISTS (SELECT 1 FROM menus WHERE code='system_jobs') THEN RAISE EXCEPTION 'jobs seed collision'; END IF;
END $$;
CREATE TABLE admin_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 kind varchar(64) NOT NULL CHECK (kind='session_cleanup'),
 status varchar(16) NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed')),
 requested_by uuid NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
 retry_of uuid UNIQUE REFERENCES admin_jobs(id) ON DELETE RESTRICT,
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
 run_after timestamptz NOT NULL DEFAULT clock_timestamp(),
 lease_until timestamptz,
 last_failure varchar(40),
 affected_rows integer NOT NULL DEFAULT 0 CHECK (affected_rows>=0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT admin_jobs_lease_check CHECK ((status='running')=(lease_until IS NOT NULL))
);
CREATE INDEX admin_jobs_claim_idx ON admin_jobs(run_after,created_at,id) WHERE status IN ('queued','running');
CREATE INDEX admin_jobs_created_idx ON admin_jobs(created_at DESC,id DESC);
CREATE TABLE admin_job_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 job_id uuid NOT NULL REFERENCES admin_jobs(id) ON DELETE RESTRICT,
 attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 3),
 status varchar(16) NOT NULL CHECK (status IN ('running','succeeded','failed')),
 failure_code varchar(40),
 affected_rows integer NOT NULL DEFAULT 0 CHECK (affected_rows>=0),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 finished_at timestamptz,
 UNIQUE(job_id,attempt),
 CHECK ((status='running')=(finished_at IS NULL))
);
CREATE INDEX admin_job_runs_job_idx ON admin_job_runs(job_id,attempt DESC);
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
 CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification','api_key','job'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
 CHECK (component_key IS NULL OR component_key IN
 ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications','api_keys','jobs'));
INSERT INTO permissions(code,name,description) VALUES
 ('jobs:view','View jobs','Read background jobs and execution history'),
 ('jobs:create','Queue jobs','Queue an approved maintenance task'),
 ('jobs:retry','Retry jobs','Retry a failed maintenance task');
INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id)
VALUES ('system_jobs','Jobs','/admin/jobs','ListTree',82,true,'active','page','jobs',true,
 (SELECT id FROM permissions WHERE code='jobs:view'));
DO $$
DECLARE n integer;
BEGIN
 UPDATE app_metadata SET schema_version=12,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=11;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'jobs version update failed'; END IF;
END $$;
COMMIT;
