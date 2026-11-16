# Database

PostgreSQL 17, UTF-8, UTC session timezone, GORM postgres driver. Connection pools
have bounded open/idle connections and connection lifetimes. Configuration is
validated before connecting. Production rejects `POSTGRES_SSLMODE=disable`.

## Schema ownership

`backend/migrations/000001_foundation.up.sql` creates only:

- `app_metadata`: UUID primary key (`gen_random_uuid()`), unique component,
  positive schema_version, created_at and updated_at TIMESTAMPTZ. One `core=1` row.
- `schema_migrations`: version/dirty state managed by golang-migrate, not GORM.

Migration 2 adds authorization tables. Migration 3 adds persistent login
protection. Migration 4 adds the controlled RBAC catalog/system-role metadata and
append-only security events. Migration 5 extends the existing menus table for
typed, fixed-component navigation and adds dashboard/menu permissions. Migration
6 adds allowlisted system settings and extends menu/audit constraints. These
migrations were applied only to isolated integration/E2E targets, not the
persistent database.
`updated_at` is initialized on insert; future writes must explicitly update it.
There is no AutoMigrate or seed account.

## Running migrations

```sh
make migrate
# Or from backend with local PostgreSQL:
go run ./cmd/migrate up
```

SQL is embedded into the migration binary. Up is idempotent: ErrNoChange is
success. Compose waits for PostgreSQL, runs the one-shot migrator, and only starts
the API after migration succeeds. CLI deliberately exposes only `up`; reviewed
down SQL exists for controlled maintenance, not accidental production rollback.
Never force a dirty version without investigating the failed SQL and backup state.

Readiness requires connectivity, a core marker matching LatestVersion(), exactly
one schema_migrations row at that same version and dirty=false. LatestVersion()
enumerates the embedded source through golang-migrate First/Next; no permanent
version constant exists in readiness. Both behind and ahead versions fail closed.
HTTP remains 503/50301 without exposing database details.

`make integration` creates random `phase21_test_<hex>` and `phase22_test_<hex>` schemas,
migrates to 1, inserts a sentinel, upgrades to the latest embedded version, verifies repeat up preserves data, constraints,
transaction-lock serialization, dirty/missing/behind/ahead rejection and down/up, then drops only that
schema. It never truncates or drops public/business data. Use a dedicated test
database account with CREATE SCHEMA privileges.

Compose stores data in `goadmin-kit_postgres_data`. A restart preserves it.
Changing POSTGRES_PASSWORD after initial volume creation does not rotate the
stored PostgreSQL password: use ALTER ROLE with controlled secret handling.

## Version 2 schema

Every table has an independently generated UUID primary key and created_at/updated_at
TIMESTAMPTZ defaults. PostgreSQL sessions use UTC. Writers must update updated_at;
there is no GORM AutoMigrate, account seed or automatic timestamp trigger.

| Table | Fields beyond shared ID/timestamps | Constraints / indexes |
| --- | --- | --- |
| admin_users | username, email, password_hash, display_name, status, auth_version, password_changed_at, last_login_at | case-insensitive unique username/email; active/disabled status; positive auth_version; status/id index |
| admin_sessions | user_id, token_hash BYTEA, user_auth_version, expires_at, revoked_at, last_seen_at | FK user CASCADE; unique 32-byte SHA-256 digest only, no raw token column; expiry after creation; active user/expiry partial index; expiry cleanup index |
| roles | code, name, description, status, is_super_admin, permissions_version | unique validated code; at most one super-admin role; positive permission version |
| permissions | code, name, description | unique resource:action code |
| user_roles | user_id, role_id | unique pair; both FK CASCADE; reverse role/user index |
| role_permissions | role_id, permission_id | unique pair; both FK CASCADE; reverse permission/role index |
| menus | parent_id, permission_id, code, name, path, icon, sort_order, visible, status | self-parent prohibited; parent/permission RESTRICT FKs; unique code; parent/sort/id and permission indexes |
| role_menus | role_id, menu_id | unique pair; both FK CASCADE; reverse menu/role index |

Auth generates 32 random bytes, stores only SHA-256 digest, and checks absolute
expiry, idle expiry, revocation, user status and auth_version on every request.
Passwords use Argon2id (16-byte random salt, 64 MiB, 3 passes, one lane, 32-byte
output; PHC encoding); unknown users undergo dummy-hash verification. Neither
password_hash nor token_hash is part of API DTOs. Disable/password-reset increments
auth_version and revokes sessions transactionally. RBAC authorization is queried live
from PostgreSQL on every request; there is no authorization cache. Menu visibility is not API authorization.

## Last super administrator

The singleton `app_metadata WHERE component='core'` row is the shared transaction
mutex. In READ COMMITTED, all future operations affecting effective super admins
(user disable/delete, membership edits, super-role disable/delete/change) MUST first
SELECT that row FOR UPDATE, then in a separate statement count distinct active
users joined to the active is_super_admin role, and reject a change leaving zero.
All paths must use the same lock order; the role uniqueness index alone does not
enforce this invariant. Bootstrap of the very first admin is a separate explicit
operator workflow, not seeded credentials. The integration test proves competing
transactions cannot acquire this guard simultaneously. Phase 2.3 applies the
business-level invariant enforcement to every IAM mutation.

The Phase 2.2 initialization CLI uses this guard and an auth_initialized metadata
marker to refuse re-initialization even after user deletion. The initial user,
super-admin role and user_roles row commit atomically.

## Version 3: login protection

`auth_login_limits` has UUID id, unique 32-byte SHA-256 key_hash of namespaced
normalized account or peer IP, window_started_at, request_count, failures,
blocked_until, created_at and updated_at (TIMESTAMPTZ). Unique keys serialize
concurrent attempts. A one-minute request window caps account traffic at 20 and
peer IP at 120. Five failures trigger exponential delays capped at 60 seconds;
15 minutes of inactivity resets the failure count. Successful login clears the
account failures in the Session transaction. Old bucket rows need a later
approved cleanup/retention job; no such job runs in this phase.

Sessions have fixed absolute expires_at, configurable idle expiry via last_seen_at
(default SESSION_IDLE_TTL=30m), and revoked_at. Successful verification updates
last_seen_at; logout revokes one and logout-all revokes all in PostgreSQL. Neither
expired Sessions nor revoked Sessions authenticate even before cleanup.

Menu cycle prevention uses service-level ancestor validation under the shared
`app_metadata(component='core')` transaction lock; the FK/self check alone does
not prevent longer cycles. Menu writes and IAM writes share this lock order.

## Version 4: RBAC and security events

Migration 4 adds `roles.is_system`, marks the bootstrap super-admin role as
system-managed, and idempotently inserts an unprivileged `administrator` system
role plus the controlled catalog: `users:view/create/update/delete/assign_roles`,
`roles:view/create/update/delete/assign_permissions`, and
`permissions:view/assign` (stored as `resource:action`, matching the immutable
Migration 2 constraint). The directory does not accept arbitrary permission
creation. It also creates append-only `security_events` with actor UUID, fixed
action code, target kind/UUID and UTC creation time; credentials and request
secrets are not event fields. Role permission assignment leaves the super role
immutable. No Editor role is seeded because no editor-facing module exists yet.

## Version 5: menu navigation

Migration 5 extends the menus table created in Migration 2 with `menu_type`
(`directory`, `page`, `external`), nullable `component_key`, and `is_system`, plus
an index over type/status/visibility. The component key is constrained to the
server-registered dashboard/users/roles/permissions/menus pages. It also expands
security event targets to include menus and inserts the dashboard and
`menus:view/create/update/delete/assign_roles` permissions idempotently. Existing
menu rows retain their data and default to page type; the application validates
their route/permission pairing before writes. No additional tables are needed:
role-to-menu grants use the existing `role_menus(role_id,menu_id)` unique pair and
cascading foreign keys.

The application supports directory nesting, fixed internal page entries and HTTPS
external links. Active/visible definitions are filtered through role-menu and
permission grants for ordinary users. Ancestor directories are included only
when they contain an authorized descendant. Super-admins see all active/visible
entries. This affects navigation only; API and page permission checks remain the
access-control boundary. Down migration 5 removes seeded system menus and its
catalog rows and is therefore destructive to those objects; it is tested only in
isolated databases and must not be used as a casual rollback.

All IAM writes serialize via the existing core metadata row lock, re-check the
acting user's current rights, and verify a positive count of active super-admin
members before commit. Disable/delete/role replacement/password reset revoke
sessions and increment auth_version transactionally. API DTOs exclude password
hashes and Session material.

## Existing database upgrades and rollback

For a fresh installation, use `go-admin init --database <exact-name>` and verify
the API reports the expected schema. Existing installations require an
operator-reviewed source/target version, restorable backup, maintenance window
and exact confirmation values. Migration 1–13 files are immutable; add a new
numbered migration for later changes. Never force dirty migration state or edit
migration metadata to bypass a failed upgrade. Down SQL is destructive by
definition and must be tested only against disposable PostgreSQL.

## Dashboard (no schema change)

Dashboard summary counts all administrators, active administrators, all roles,
active roles and existing `security_events` with a single SQL statement. Activity
reads order by `(created_at DESC, id DESC)` with a page size of 1–50 and page
index of 1–1000. The count and page are separate read statements, so concurrent
new events may change the total between them. Trend endpoints filter events in
UTC half-open ranges `[start,end)` for seven or thirty calendar days ending
after the current UTC date, then zero-fill missing UTC dates. No login outcomes
are represented by these events. No migration was created or applied for 3.1;
the latest embedded version was v5 at 3.1 acceptance.

## System settings (embedded v6)

`system_settings` has a generated UUID primary key, unique validated namespaced
`key`, JSONB `value_json` for nonsensitive values, `is_sensitive`, optional
`value_ciphertext BYTEA` and `encryption_key_version`, plus UTC `created_at` and
`updated_at` TIMESTAMPTZ. A CHECK enforces exactly one storage representation:
public JSON without ciphertext/version, or secret ciphertext with positive key
version and no public JSON. The application accepts only these keys and types:
`site.name` (nonblank <=100 chars), `site.description` (<=500),
`branding.logo_path` (same-origin literal path or empty),
`localization.timezone` (UTC or valid IANA zone), `localization.language`
(`en`, `zh-CN`, `zh-TW`), and `general.default_page_size`
(`10`, `25`, `50`, `100`). Defaults are returned without inserting rows.
Preferences are stored, not retroactively applied to legacy pages or server
timezone. Future secret keys must be explicitly registered in code; ciphertext
is AES-256-GCM with random nonce and key version 1, using a deployment-provided
base64 32-byte key. Keep the key with encrypted backups and design key rotation
before registering any production secret setting. Missing/invalid keys fail
closed; no secret key is currently exposed by the API. V6 also adds
`settings:view/update` catalog entries and permits `setting` audit targets and
the `settings` menu component. The down migration removes v6 rows, permissions,
setting audit events and settings menus: destructive and only tested in isolated
databases, never an automatic production rollback.

## Private Media schema (embedded v7)

Migration 7 creates `media_files` with UUID PK, unique UUID
`upload_operation_id`, unique 32-hex opaque `storage_key`, validated display
name/extension/detected MIME, `byte_size`, 32-byte SHA-256, optional width/height,
nullable uploader FK, private visibility, status (`pending`, `ready`, `deleting`,
`delete_failed`, `deleted`, `failed`, `cleanup_failed`, `unavailable`), verification
generation, bounded retry/claim fields and UTC timestamps. Partial indexes cover
ready ordering/type/uploader and scheduled recovery. No raw bytes or client path
are stored in PostgreSQL. `media_storage_binding` pairs a single DB root UUID
with the private volume marker, active verification generation, state and scan
cursors. Startup only opens ready rows validated in the current generation;
missing/corrupt files become unavailable. Failed deletion remains recoverable;
deleted rows stay as tombstones. `system_settings.media_file_id` restricts a Logo
reference to an existing Media row, while code validates ready image status.
Writes acquire the core metadata row before any media row, including Logo
assignment/deletion. Catalog adds media:view/upload/delete, media audit targets
and the media component. Down 7 rejects any media history, root binding, media
events, grants or menu references; it never clears tombstones automatically.

PostgreSQL and the corresponding private Media volume must be backed up and
restored as one consistent generation. Verify both restore independently; bind
an empty root only after confirming the exact DB and administrator. Never run an
API against an unbound or mismatched root. A root's objects are not public assets.

## Audit and login logs schema (embedded v8)

Migration 8 adds nullable `request_method`, Gin route-template `request_path`,
trusted peer-derived `client_ip INET`, allowlisted browser-family `user_agent`
and classified `outcome` to existing `security_events` without rewriting old
rows. The read API maps old NULL outcomes to `success`; old request context
remains absent. Indexes on `(created_at DESC,id DESC)` and
`(action,created_at DESC,id DESC)` bound recent operation reads.

`login_events` has a generated UUID PK, nullable `user_id` FK (`ON DELETE SET
NULL`), `account_kind` (`known`/`unknown`), `outcome`
(`success`/`failure`/`rate_limited`), optional `client_ip INET`, allowlisted
`user_agent`, and UTC `created_at`. Unknown user input, passwords, hashes,
Session/CSRF tokens and full user agents are never stored. The absent user ID
of an unknown or rate-limited attempt is intentionally not reversible to a
username. Auth inserts events in the same transaction as successful Session
creation/reset or failed-attempt accounting; rate-limited attempts persist
with the request buckets. Time/user/outcome indexes support bounded lists.
No application API updates or deletes logs. There is no automatic retention
or background cleanup; operational retention and legal policy require a
separate decision. Down 8 refuses any login history, enriched operation event,
role grant or log menu; a production rollback requires a reviewed data plan.
Catalog adds `audit:view`, `login_logs:view`, and two controlled menu component
keys. Apply changes through the reviewed, versioned migrator.

## Profile (no schema change)

Profile reuses the v8 `admin_users`, `admin_sessions`, `auth_login_limits` and
`security_events` structures. It adds no migration, permission catalog row or
session metadata. The current actor is always derived from the live Session;
there is no API accepting a target user ID. Profile mutations use the existing
core metadata lock before live user/Session validation, matching IAM's write
order. Profile/password and session-revocation audit events commit in the same
transaction as their corresponding changes.

Password changes increment `admin_users.auth_version` and revoke every active
`admin_sessions` row atomically. A persistent account-scoped reauthentication
counter uses a domain-separated SHA-256 key in `auth_login_limits`, allows at
most five current-password checks per minute, and includes successful checks;
no raw account identifier or password is added to that table. Session listing
uses bounded pagination and filters revoked, absolute-expired, idle-expired or
auth-version-stale Sessions. Only UUID and UTC created/last-seen/expiry times
are returned. There is no token, token hash, IP or user-agent field in Profile
responses.

## Migration history

Migration files are implemented through v13. A generated project initializes
an empty database with the complete baseline. Existing databases require exact
source/target version and backup confirmations before upgrades.

| Module | SQL migration | Data and constraints |
| --- | --- | --- |
| Dashboard | None | Bounded UTC aggregates over existing users, roles and security events |
| Settings | v6 | `system_settings`, controlled permissions, audit target and menu component |
| Media | v7 | `media_files`, storage binding, Logo FK, permissions and audit/menu constraints |
| Audit/Login Logs | v8 | Additive security-event context and classified login outcomes |
| Profile | None | Reuses users, sessions, login limits and security events |
| Articles demo | v9 | Separate categories/articles tables, slug uniqueness, category/author FKs, draft/published state and bounded-search indexes |
| Notifications | v10 | Actor-scoped inbox, recipients and durable read state; management permission and menu seeds |
| API keys | v11 | SHA-256 token hashes, owner/revocation/expiry and limited Dashboard scope; raw keys returned only once |
| Jobs | v12 | Durable job status, bounded claims/retries and execution records; explicit registered task kinds |
| Local Plugin System | v13 | `admin_plugins`, `admin_plugin_config`, `admin_plugin_migrations`, append-only `admin_plugin_events`; plugin RBAC/menu seeds; extend controlled audit target and menu component checks |

Migration 1–13 are the released template baseline. Verify upgrade from the
documented source version, repeat execution, dirty/ahead/behind readiness and
rollback guards in isolated PostgreSQL. Sensitive settings and audit/login events
must never store passwords, raw Session tokens or CSRF tokens. Existing databases
require an explicit target, validated backup and operator approval before upgrade.

### Migration 13 — Local Plugin System

Migration 13 strictly requires core schema v12 and registers seven system
permissions plus the `system_plugins` menu. Plugin IDs, status, signed manifest
digest, publisher key ID, configuration, applied migration checksums and event
history are stored separately. `admin_plugin_config` stores either JSONB public
data or encrypted secret bytes, never both. Plugin migration and config rows
cascade from registry removal; plugin lifecycle events intentionally have no
plugin FK and remain after uninstall. Down requires v13 and refuses while any
plugin registry/configuration/migration/event, plugin security event, system
permission grant, external plugin-permission menu reference, role grant or child
menu exists. It validates the system seeds before restoring prior CHECK
constraints and schema version 12. A fresh database reaches schema version 13;
rollback is deliberately blocked when plugin data or references exist.
