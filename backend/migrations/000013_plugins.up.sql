BEGIN;
DO $$
DECLARE v integer;
BEGIN
 SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
 IF NOT FOUND OR v IS DISTINCT FROM 12 THEN RAISE EXCEPTION 'plugins require core schema version 12'; END IF;
 IF EXISTS (SELECT 1 FROM permissions WHERE code IN
 ('plugins:view','plugins:install','plugins:enable','plugins:disable','plugins:uninstall','plugins:configure','plugins:migrate'))
 OR EXISTS (SELECT 1 FROM menus WHERE code='system_plugins') THEN
   RAISE EXCEPTION 'plugin seed collision';
 END IF;
END $$;

CREATE TABLE admin_plugins (
 plugin_id varchar(80) PRIMARY KEY CHECK (plugin_id ~ '^[a-z][a-z0-9-]{2,79}$'),
 name varchar(120) NOT NULL,
 version varchar(64) NOT NULL,
 description varchar(1000) NOT NULL DEFAULT '',
 author varchar(120) NOT NULL DEFAULT '',
 source_key varchar(128) NOT NULL,
 manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest)='object'),
 manifest_sha256 bytea NOT NULL CHECK (octet_length(manifest_sha256)=32),
 status varchar(16) NOT NULL CHECK (status IN ('installed','enabled','disabled','failed')),
 failure_code varchar(64),
 installed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX admin_plugins_status_idx ON admin_plugins(status,plugin_id);

CREATE TABLE admin_plugin_config (
 plugin_id varchar(80) NOT NULL REFERENCES admin_plugins(plugin_id) ON DELETE CASCADE,
 config_key varchar(120) NOT NULL CHECK (config_key ~ '^[a-z][a-z0-9_.-]{0,119}$'),
 public_value jsonb,
 secret_ciphertext bytea,
 key_version smallint,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(plugin_id,config_key),
 CHECK ((public_value IS NOT NULL)::integer + (secret_ciphertext IS NOT NULL)::integer = 1),
 CHECK ((secret_ciphertext IS NULL) = (key_version IS NULL))
);

CREATE TABLE admin_plugin_migrations (
 plugin_id varchar(80) NOT NULL REFERENCES admin_plugins(plugin_id) ON DELETE CASCADE,
 version integer NOT NULL CHECK (version > 0),
 checksum bytea NOT NULL CHECK (octet_length(checksum)=32),
 applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(plugin_id,version)
);

-- Events intentionally have no plugin FK: uninstall must not erase the audit trail.
CREATE TABLE admin_plugin_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 plugin_id varchar(80) NOT NULL CHECK (plugin_id ~ '^[a-z][a-z0-9-]{2,79}$'),
 plugin_version varchar(64) NOT NULL DEFAULT '',
 actor_user_id uuid REFERENCES admin_users(id) ON DELETE SET NULL,
 event_type varchar(24) NOT NULL CHECK (event_type IN ('install','enable','disable','uninstall','upgrade','configure','error')),
 outcome varchar(12) NOT NULL CHECK (outcome IN ('succeeded','failed')),
 failure_code varchar(64),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX admin_plugin_events_lookup_idx ON admin_plugin_events(plugin_id,created_at DESC,id DESC);

ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
 CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification','api_key','job','plugin'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
 CHECK (component_key IS NULL OR component_key IN
 ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications','api_keys','jobs','plugins'));

INSERT INTO permissions(code,name,description) VALUES
 ('plugins:view','View plugins','Read installed plugin metadata and event history'),
 ('plugins:install','Install plugins','Install a trusted local plugin package'),
 ('plugins:enable','Enable plugins','Enable a registered plugin and its routes'),
 ('plugins:disable','Disable plugins','Disable a registered plugin and its routes'),
 ('plugins:uninstall','Uninstall plugins','Remove an installed plugin while retaining event history'),
 ('plugins:configure','Configure plugins','Update plugin configuration'),
 ('plugins:migrate','Migrate plugins','Apply signed plugin database migrations');
INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id)
VALUES ('system_plugins','Plugins','/admin/plugins','Puzzle',86,true,'active','page','plugins',true,
 (SELECT id FROM permissions WHERE code='plugins:view'));

DO $$
DECLARE n integer;
BEGIN
 UPDATE app_metadata SET schema_version=13,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=12;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'plugins version update failed'; END IF;
END $$;
COMMIT;
