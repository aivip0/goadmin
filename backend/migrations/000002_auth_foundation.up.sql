BEGIN;

CREATE TABLE admin_users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username varchar(64) NOT NULL CHECK (username = btrim(username) AND length(username) > 0),
    email varchar(254) NOT NULL CHECK (email = btrim(email) AND length(email) > 0),
    password_hash text NOT NULL CHECK (length(password_hash) > 0),
    display_name varchar(128) NOT NULL DEFAULT '',
    status varchar(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    auth_version bigint NOT NULL DEFAULT 1 CHECK (auth_version > 0),
    password_changed_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX admin_users_username_unique ON admin_users (lower(username));
CREATE UNIQUE INDEX admin_users_email_unique ON admin_users (lower(email));
CREATE INDEX admin_users_status_idx ON admin_users (status, id);

CREATE TABLE admin_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    user_auth_version bigint NOT NULL CHECK (user_auth_version > 0),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_seen_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX admin_sessions_user_active_idx ON admin_sessions (user_id, expires_at) WHERE revoked_at IS NULL;
CREATE INDEX admin_sessions_expiry_idx ON admin_sessions (expires_at);

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code varchar(64) NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]*$'),
    name varchar(128) NOT NULL CHECK (length(btrim(name)) > 0),
    description text NOT NULL DEFAULT '',
    status varchar(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    is_super_admin boolean NOT NULL DEFAULT false,
    permissions_version bigint NOT NULL DEFAULT 1 CHECK (permissions_version > 0),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX roles_single_super_idx ON roles (is_super_admin) WHERE is_super_admin;

CREATE TABLE permissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code varchar(128) NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$'),
    name varchar(128) NOT NULL CHECK (length(btrim(name)) > 0),
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, role_id)
);
CREATE INDEX user_roles_role_idx ON user_roles (role_id, user_id);

CREATE TABLE role_permissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (role_id, permission_id)
);
CREATE INDEX role_permissions_permission_idx ON role_permissions (permission_id, role_id);

CREATE TABLE menus (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id uuid REFERENCES menus(id) ON DELETE RESTRICT,
    permission_id uuid REFERENCES permissions(id) ON DELETE RESTRICT,
    code varchar(64) NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]*$'),
    name varchar(128) NOT NULL CHECK (length(btrim(name)) > 0),
    path varchar(512) NOT NULL DEFAULT '',
    icon varchar(64) NOT NULL DEFAULT '',
    sort_order integer NOT NULL DEFAULT 0,
    visible boolean NOT NULL DEFAULT true,
    status varchar(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX menus_parent_sort_idx ON menus (parent_id, sort_order, id);
CREATE INDEX menus_permission_idx ON menus (permission_id);

CREATE TABLE role_menus (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    menu_id uuid NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (role_id, menu_id)
);
CREATE INDEX role_menus_menu_idx ON role_menus (menu_id, role_id);

-- Future super-admin mutations lock this existing singleton row before counting active members.
UPDATE app_metadata SET schema_version = 2, updated_at = CURRENT_TIMESTAMP WHERE component = 'core';
COMMIT;
