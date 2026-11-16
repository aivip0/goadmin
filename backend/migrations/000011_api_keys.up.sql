BEGIN;
DO $$
DECLARE v integer;
BEGIN
 SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
 IF NOT FOUND OR v IS DISTINCT FROM 10 THEN RAISE EXCEPTION 'API keys require core schema version 10'; END IF;
 IF EXISTS (SELECT 1 FROM permissions WHERE code IN ('api_keys:view','api_keys:create','api_keys:revoke'))
 OR EXISTS (SELECT 1 FROM menus WHERE code='system_api_keys') THEN RAISE EXCEPTION 'API key seed collision'; END IF;
END $$;

CREATE TABLE admin_api_keys (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
 name varchar(100) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
 token_prefix varchar(16) NOT NULL,
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
 scopes text[] NOT NULL CHECK (scopes = ARRAY['dashboard:view']::text[]),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 last_used_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK (expires_at>created_at)
);
CREATE INDEX admin_api_keys_owner_time_idx ON admin_api_keys(owner_user_id,created_at DESC,id DESC);
CREATE INDEX admin_api_keys_active_idx ON admin_api_keys(expires_at) WHERE revoked_at IS NULL;
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
 CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification','api_key'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
 CHECK (component_key IS NULL OR component_key IN
 ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications','api_keys'));
INSERT INTO permissions(code,name,description) VALUES
 ('api_keys:view','View API keys','List own API keys'),
 ('api_keys:create','Create API keys','Issue a scoped API key'),
 ('api_keys:revoke','Revoke API keys','Revoke own API keys');
INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id)
VALUES ('system_api_keys','API Keys','/admin/api-keys','KeyRound',81,true,'active','page','api_keys',true,
 (SELECT id FROM permissions WHERE code='api_keys:view'));
DO $$
DECLARE n integer;
BEGIN
 UPDATE app_metadata SET schema_version=11,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=10;
 GET DIAGNOSTICS n = ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'API key version update failed'; END IF;
END $$;
COMMIT;
