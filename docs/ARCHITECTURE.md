# Architecture

The starter keeps one Go API, one Next.js application and one PostgreSQL database.
Administrator authentication, users/roles/permissions, menu management and
role-aware dynamic navigation are implemented. The generic operational dashboard,
allowlisted settings, private Media, audit/login logs and actor-scoped Profile are
implemented, together with Articles, notifications, scoped API keys, durable jobs
and the trusted local plugin system.

Browser -> Next.js same-origin explicit `/api/v1/*` allowlist -> server-only Go API URL ->
PostgreSQL connection pool and migration metadata. Next.js never accesses PostgreSQL.

The Go entrypoint only loads configuration, logs and process signals. Bootstrap
owns the database pool, HTTP server timeouts and graceful shutdown. Router owns
public foundation routes. Core packages hold configuration, logs, response
envelopes and recovery. GORM opens/owns the bounded PostgreSQL pool; Auth uses
parameterized `database/sql` transactions on that pool for explicit row locks.
golang-migrate owns schema; GORM AutoMigrate is not used.

Auth follows Handler -> Service -> Repository -> PostgreSQL. Later modules follow this layering. No service
or repository wrappers are added around a simple infrastructure health probe.
`internal/modules/auth`, `internal/modules/iam`, `internal/modules/menus` and
`internal/modules/dashboard`, `settings`, `media`, `logs` and `profile` contain
the implemented aggregate-read, settings, private local storage, protected log
and self-service profile behavior. Articles, notifications, API keys, jobs and
plugins use the same boundaries and register through the existing bootstrap.

Browser assets contain no database credentials. The Go process fails startup if
PostgreSQL cannot connect; readiness checks both connectivity and migration state.
Liveness is independent of PostgreSQL. All JSON health responses are non-cacheable.

UI conventions: neutral surfaces, semantic green/red states, shared CSS tokens,
light/dark theme, keyboard focus and reduced-motion support. Buttons use the
local shadcn-compatible primitive and Lucide icons. No marketing landing page.

## Authentication boundary

The catch-all Route Handler is not an open proxy: lib/backend.ts validates exact
infrastructure/auth paths and an explicit users/roles/permissions/menus path allowlist.
Only GET/POST/PUT/PATCH/DELETE are exported; auth paths have specific method rules.
Origin, Fetch Metadata, CSRF header and the two application cookies reach Go.
Host, Forwarded, X-Forwarded-*, X-Real-IP, Authorization and browser-supplied
request IDs do not. Go generates the authoritative request ID; Next returns it.
Cookie attributes are owned by Go; Next preserves separate Set-Cookie headers.
No browser-controlled URL can select an upstream. Request bodies are capped at 1 MiB;
upstream work including response reads times out after 5 seconds; redirects fail closed.
Binary bodies and empty 204 responses are preserved. No credential/body logging.

Core security validates exact public origins and signed double-submit CSRF tokens
before every unsafe business API request (including login and unregistered paths).
Health/OpenAPI behavior remains unchanged. Auth registers login, me, logout and
logout-all after the existing router middleware. The session Cookie is set only
after a SQL transaction commits its SHA-256 digest. Every authentication reads
the session, expiry, status and auth_version from PostgreSQL; no identity cache.
User row locks serialize login, verification and logout-all. `/login` and `/admin`
use the same-origin proxy, and `/admin` calls me on load before rendering private
account data. A 401 clears memory state and redirects. The public status page
remains accessible.

The explicit admin CLI locks the core metadata row and atomically inserts the
first user, super-admin role, membership and durable auth_initialized marker.
Argon2id checks on nonexistent users use a dummy hash. Persistent login buckets
are in auth_login_limits; credentials, cookie contents and hashes are never logged.

## Users and RBAC

`internal/modules/iam` follows Handler -> Service -> Repository and shares the
existing PostgreSQL pool with Auth. Management routes validate the opaque Session
against PostgreSQL, then evaluate active user -> active role -> role permissions
from the database on every request. There is no permission cache; revocation is
visible on the next request. Super-admin membership is a database role fact,
never a request-body flag. Every registered management route declares a
permission; unregistered routes remain closed by Gin's 404. The UI reads
effective permissions to shape navigation, while Go remains the authorization
boundary.

## Menus and navigation

`internal/modules/menus` follows Handler -> Service -> Repository and uses the
existing PostgreSQL pool. Management endpoints separately enforce the live
`menus:view/create/update/delete/assign_roles` permissions. The `my` endpoint
builds a sorted tree from active, visible menu definitions and role assignments;
it includes eligible ancestors of assigned pages and omits empty directories.
The super-admin role sees all active visible menus. Menu visibility is navigation
only and never grants page or API access; destination page permissions and Go API
authorization remain independently enforced.

Page menus select a server-controlled component key, route and matching permission
from a fixed registry. External menus require HTTPS; icons are allowlisted. Parent
directories, cycles, role assignments and reorder operations are checked inside
serialized database writes. The Next.js shell loads `/menus/my` with current
identity and effective permissions, renders nested navigation, and uses the same
origin proxy. System navigation seeds are idempotent and do not overwrite edits.

Every IAM write locks `app_metadata(component='core') FOR UPDATE` before any
user/role membership mutation, checks current actor authority under that lock,
applies the change, counts active users holding the active super role in a
separate statement, and commits only if at least one remains. Audit events share
the same transaction. Role permission assignments are super-admin-only; a
non-super administrator can assign only non-system roles whose permissions are
a subset of their own and cannot change their own role set.

Gin trusts no proxies by default. Explicit IP/CIDR configuration is validated;
wildcard networks are rejected. ClientIP() examines X-Forwarded-For only when the
socket peer is trusted, walking the chain from the trusted end. Forwarded and
X-Real-IP are never identity sources. The Next proxy deliberately does not assert
a browser IP, so Go sees Next's peer IP for proxied traffic. Before login rate
limiting, deploy a controlled gateway that strips/rebuilds forwarding headers and
review the full trust chain; never trust an arbitrary browser header or per-user
identity to derive an anonymous login IP. Local access works without proxy trust.
The account bucket (20 requests/minute) is reliable through Next; the 120/minute
peer-IP bucket currently groups browser traffic at Next's peer address.

Migration state is compared with the latest embedded golang-migrate source version.
Old, ahead, dirty or missing migration state is not ready. API startup never runs
migrations automatically; use the CLI with an explicitly named database.

## Implemented application modules

Dashboard, Settings, Media, Logs, Profile and Articles endpoints/pages are
implemented and functionally accepted. This is not production deployment approval:
Media requires explicit private-root binding and verified paired backups; logs
require an operational retention and durability policy. Existing
Auth/IAM/Menus services, Session/CSRF middleware, response envelope and route
registration remain the integration points. New modules follow the existing
Handler -> Service -> Repository boundary and register through bootstrap;
`database/sql` transactions may continue to handle explicit locks while the
GORM-managed pool remains the sole PostgreSQL connection owner. The dashboard
aggregates administrator, role, security and classified `login_events` rows,
without fabricated metrics. Its three GET routes validate the active database
Session and live `dashboard:view` permission in Go; its UTC trends use bounded
7/30-day ranges and activities use bounded pagination. Readiness is reported
as a safe ready/unavailable category, not infrastructure details. Settings use
a typed, allowlisted registry: `GET` requires `settings:view`, `PUT` requires
`settings:update`, both validate the live database Session and role graph. A
mutation takes the core metadata row lock, rechecks authority, atomically upserts
the selected rows and records one `security_events` entry per setting. The common
CSRF/Origin middleware protects PUT; the proxy admits only exact GET/PUT paths.
Public values have JSONB storage; future registered secret values use a separate
AES-256-GCM nonce+ciphertext BYTEA, key version, and a 32-byte deployment key.
Missing/wrong key fails closed, and GET returns only a configured flag for such
values. No secret-valued key is registered in 3.2. Settings do not change the
process, database timezone, auth policy or existing list pagination. The
responsive editor validates inputs and warns about unsaved changes. The menu
registry includes a settings entry; menu visibility alone grants no API access.

Media is an opt-in Gin module registered through bootstrap only when
`MEDIA_STORAGE_ROOT` is set. Handler checks the live Session and media permission;
Service coordinates pending/ready/deleting/tombstone transitions; SQL Repository
owns metadata/transactional audit; Local Storage owns private file bytes. A
dedicated Docker volume is mounted only by the API. The root marker and DB binding
must match; an exclusive PostgreSQL advisory connection prevents a second API
instance from serving the same DB. Startup rotates the verification generation
and verifies every ready object before opening the Media gate. The bounded
`admin media-reconcile` command uses its own lock and no separate worker.
Next's Node route admits only exact Media paths and streams multipart/binary
responses with byte limits; the existing JSON proxy remains bounded at 1 MiB.
All Media access remains private and requires live RBAC; PDF/TXT are download-only.
The S3-compatible provider is an interface-level future extension only. The
Logs module is registered through bootstrap and exposes bounded GET list/detail
routes with a fresh Session and live `audit:view` or `login_logs:view` check on
each call. Existing IAM, Menu and Settings mutations enrich transactional
`security_events` with route-template request context; Auth writes classified
login outcomes in the same transaction as failure limits or successful session
creation. Unknown account names, credentials, tokens, raw user agents and raw
query strings are not stored. Next admits only exact log routes; pages use the
existing shell, RBAC navigation and no-store API. Log history has no mutation
API and no silent retention deletion.

The Profile module operates only on the current database Session/account, with
no administrator-wide permission or accepted user ID in its routes. Writes
share the IAM lock order (`app_metadata core` -> user -> Session), revalidate
the live Session in the transaction, and commit with audit events. Password
change verifies the current Argon2id hash, uses a durable per-user reauthentication
limit in `auth_login_limits`, increments `auth_version`, revokes all Sessions
and writes its audit record atomically. Session listings expose only UUID and
creation/activity/expiry timestamps; they never expose token hashes, tokens,
IP or guessed device identity. Changes to email/display name, password and
Session revocations are immediately visible because there is no cache. All
mutations use the existing CSRF guard; the browser holds no Session credential
in Web Storage. Next's allowlist admits only the fixed Profile paths/methods.
No schema migration or new permission was required.

Articles is an optional example business module kept separate from generic
identity and infrastructure modules. Remove it only through a reviewed fork; do
not repurpose its tables for another business domain.

## Local Plugin System

`internal/modules/plugins` follows Handler -> Service -> Repository. Migration
13 adds registry, isolated public/secret configuration, per-plugin migration
checksums and append-only lifecycle events. Plugin management has seven dedicated
RBAC permissions. Each management API checks the live Session and operation
permission; plugin route requests additionally resolve the permission from the
stored manifest and check it on every call. Plugin menu rows are registered
transactionally, use the fixed `plugins` component key, and become active only
when enabled. Navigation additionally requires the corresponding plugin
permission and never grants API access.

Installation source is a local read-only package tree
`<PLUGIN_PACKAGE_ROOT>/<plugin-id>/<semver>/`. A signed raw manifest is checked
against configured Ed25519 publisher keys and every SQL migration is pinned by
SHA-256 in that manifest. The system never loads package Go binaries, scripts,
JavaScript, templates or dynamic imports. Runtime route behavior is available
only through an optional provider registered in application code at build
time through `bootstrap.RunWithPluginExtensions`; without one the route reports
provider-unavailable. Duplicate extension IDs fail startup rather than
overwriting an existing provider. Lifecycle hooks are
build-time preflight validators only. Signed SQL migrations are trusted
publisher input and run in the same PostgreSQL transaction as registry,
permission and menu registration; this is not a sandbox against a compromised
trusted publisher.

Plugin configuration is keyed by plugin ID. Public JSON and secret ciphertext
are mutually exclusive; secrets use AES-256-GCM with a separate deployment key
and key version, with authenticated additional data binding ciphertext to its
plugin ID and configuration key. Secret values are redacted from API reads. Plugin event rows
survive uninstall. Uninstall is allowed only while disabled and does not reverse
plugin DDL; migrations require a future explicit, reviewed rollback strategy.

All Go endpoints independently enforce live RBAC or clearly scoped self-service
authorization. Adding a menu only affects navigation; it never grants API access.
Integration and browser tests should use an isolated PostgreSQL database and
dedicated API/Next processes, not a shared or production database.
