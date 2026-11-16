BEGIN;
DO $$
DECLARE v integer;
BEGIN
  SELECT schema_version INTO v FROM app_metadata WHERE component='core' FOR UPDATE;
  IF NOT FOUND OR v IS DISTINCT FROM 9 THEN
    RAISE EXCEPTION 'notifications migration requires core schema version 9';
  END IF;
  IF EXISTS (SELECT 1 FROM permissions WHERE code IN ('notifications:view','notifications:create'))
    OR EXISTS (SELECT 1 FROM menus WHERE code='system_notifications') THEN
    RAISE EXCEPTION 'notifications seed collision';
  END IF;
END $$;

CREATE TABLE admin_notifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  recipient_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
  sender_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
  title varchar(160) NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 160),
  body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 4000),
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX admin_notifications_recipient_time_idx ON admin_notifications(recipient_user_id,created_at DESC,id DESC);
CREATE INDEX admin_notifications_unread_idx ON admin_notifications(recipient_user_id,created_at DESC) WHERE read_at IS NULL;

ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
  CHECK (target_type IN ('user','role','menu','setting','media','article','article_category','notification'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
  CHECK (component_key IS NULL OR component_key IN
    ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories','notifications'));
INSERT INTO permissions(code,name,description) VALUES
  ('notifications:view','View notifications','Read own notifications'),
  ('notifications:create','Send notifications','Send a notification to an administrator');
INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id)
VALUES ('system_notifications','Notifications','/admin/notifications','Bell',80,true,'active','page','notifications',true,
  (SELECT id FROM permissions WHERE code='notifications:view'));
DO $$
DECLARE n integer;
BEGIN
  UPDATE app_metadata SET schema_version=10,updated_at=CURRENT_TIMESTAMP WHERE component='core' AND schema_version=9;
  GET DIAGNOSTICS n = ROW_COUNT;
  IF n <> 1 THEN RAISE EXCEPTION 'notifications version update failed'; END IF;
END $$;
COMMIT;
