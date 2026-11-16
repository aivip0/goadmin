# API foundation

Go base: `http://localhost:8080`; prefix `/api/v1`. Health and OpenAPI are
deliberately public infrastructure endpoints, not a placeholder auth bypass.

| Method | Path | Meaning |
| --- | --- | --- |
| GET | /api/v1/health/live | process/router alive; no database query |
| GET | /api/v1/health | database + migration readiness |
| GET | /api/v1/health/ready | readiness alias |
| GET | /api/v1/openapi.json | embedded OpenAPI 3.0.3 contract |
| GET | /api/v1/auth/csrf | signed CSRF token + HttpOnly CSRF Cookie |

Success envelope:

```json
{"code":0,"message":"success","data":{"status":"ok","database":"connected","migration":"current","checked_at":"2026-11-12T03:00:00Z"}}
```

Unavailable readiness returns HTTP 503, code 50301 and data null. Recovery returns
500/50000; missing route 404/40400; unsupported method 405/40500. Errors omit
database details and secrets. API responses include a generated X-Request-ID.
OpenAPI JSON is the deliberate non-envelope endpoint to support standard tooling.

Next.js exposes the same-origin explicit-path server-side proxy described below.
Upstream HTTP status is preserved; timeout/connection/redirect failure becomes 503/50302.
The upstream URL is runtime server-only `API_INTERNAL_URL`. The proxy has no
user-controlled upstream URL/path. `/health` on Next.js is process liveness only.

The contract is validated with Swagger Parser in frontend unit tests. Auth,
users/roles/permissions and menu handlers are registered.

## Authentication communication contract

Browser requests use relative URLs and `credentials: 'same-origin'` (fetch default).
Session tokens never appear in JSON, localStorage or JavaScript-readable cookies.
Call GET /api/v1/auth/csrf: data is `{csrf_token: string, expires_in: number}`.
Keep this token only in memory; send X-CSRF-Token on POST/PUT/PATCH/DELETE together
with the browser Cookie and exact allowed Origin. The Go middleware checks all
unsafe business API methods, even paths not implemented yet. Missing, expired,
invalid, session-mismatched tokens or absent/null/foreign Origin return 403/40301.
Sec-Fetch-Site: cross-site is rejected, including CSRF issuance. GET issuance may
omit Origin for same-origin navigation/fetch, but a supplied Origin must be valid.
No permissive CORS headers are emitted. Safe methods must never mutate business data.

Implemented administrator endpoints:

| Method/path | Request | Success (code 0 envelope) |
| --- | --- | --- |
| POST /api/v1/auth/login | application/json `{username, password}` + CSRF/Origin | data `{user: {id, username, email, display_name}}`; server sets opaque session Cookie and clears CSRF Cookie |
| POST /api/v1/auth/logout | no body required + CSRF/Origin | data null; revoke current DB session, expire both Cookies |
| POST /api/v1/auth/logout-all | no body required + CSRF/Origin | data null; revoke all administrator sessions |
| GET /api/v1/auth/me | session Cookie | data `{user: {id, username, email, display_name}}` |

After login/logout fetch a new CSRF token because it is bound to the current
session cookie. Login failure returns generic 401/40101 without identifying
whether a username exists. Invalid request returns 400/40000. Missing or invalid
session returns 401/40100. Persistent limits are checked before password hashing;
429/42900 includes Retry-After. Passing CSRF is NOT authentication; logout-all
requires a valid session. Logout is idempotent after CSRF succeeds. Every auth
response is no-store. No raw session token or password is returned in JSON.

Proxy allowlist: GET on health/live/ready, OpenAPI, auth/csrf and auth/me; POST on
auth/login, auth/logout and auth/logout-all; admin methods are accepted only on
explicitly enumerated implemented users, roles and permissions collection/item/
action paths. This reserves only the
listed transport paths, not arbitrary CRUD semantics.
No arbitrary nested route, encoded path, foreign URL, upstream parameter or method
is accepted. Query strings are data only and cannot change the fixed destination.
Only application cookies go upstream; duplicate auth cookie names are rejected.
The proxy preserves body bytes, Content-Type, status, multiple Set-Cookie,
X-Request-ID and Retry-After; strips all inbound forwarding/identity headers.
Ordinary request bodies over 1 MiB return 413/41300; explicit Media paths use a
separate bounded streaming Node proxy. Responses are no-store. Non-JSON and empty
responses are supported.

## Cookie policy

Host-only goadmin_session and goadmin_csrf, Path=/, HttpOnly, SameSite=Lax by
default (Strict supported; None rejected). Session defaults to 12h, CSRF to 1h;
both carry Max-Age and Expires. Database idle expiry defaults to 30m and is
configurable independently up to SESSION_TTL. Clearing uses the same attributes, empty value,
Max-Age=0 on the wire and a past Expires. Production requires Secure/HTTPS and an
independently generated CSRF_SECRET of at least 32 bytes. Development without a
secret uses a fresh per-process random key: restart invalidates CSRF tokens.
Multiple API replicas must share that secret. Tokens contain timestamp, 32 random
bytes and an HMAC-SHA256 signature binding timestamp/nonce to session cookie;
cookie/header/signature comparisons are constant-time. No token logging.

## Public error catalog

All error data is null; raw SQL/configuration/path/password errors are never serialized.

| HTTP | Code | Message |
| --- | --- | --- |
| 400 | 40000 | Invalid request |
| 401 | 40100 | Authentication required |
| 401 | 40101 | Invalid username or password |
| 403 | 40300 / 40301 | Permission denied / CSRF validation failed |
| 404 | 40400 | Resource not found |
| 405 | 40500 | Method not allowed (existing infrastructure) |
| 409 | 40900 | Resource conflict |
| 413 | 41300 | Request body too large (proxy) |
| 422 | 42200 | Validation failed |
| 429 | 42900 | Too many requests |
| 500 | 50000 | Internal server error |
| 503 | 50301 / 50302 | Service not ready / API unavailable (proxy) |

Rate-limit handlers must set Retry-After when introduced. Pagination retains
page/page_size. Health envelope/status remain unchanged.

## Users and RBAC APIs

All routes below require a valid Session. Their policy is evaluated against the
live PostgreSQL role graph on each request. Mutations also require the standard
Origin + CSRF checks. The acting administrator comes only from the validated
Session, never a request parameter.

| Method/path | Required permission | Purpose |
| --- | --- | --- |
| GET/POST `/api/v1/admin/users` | `users:view` / `users:create` | Paginated list / create with initial password |
| GET/PUT/DELETE `/api/v1/admin/users/:id` | `users:view/update/delete` | Safe detail / profile update / delete |
| POST `/api/v1/admin/users/:id/enable` or `/disable` | `users:update` | Change status; disable revokes Sessions |
| PUT `/api/v1/admin/users/:id/password` | `users:update` | Reset password and revoke Sessions |
| PUT `/api/v1/admin/users/:id/roles` | `users:assign_roles` | Replace role membership atomically |
| GET/POST `/api/v1/admin/roles` | `roles:view` / `roles:create` | List / create custom role |
| GET/PUT/DELETE `/api/v1/admin/roles/:id` | `roles:view/update/delete` | Role detail / edit / delete unused custom role |
| PUT `/api/v1/admin/roles/:id/permissions` | `roles:assign_permissions` | Replace grants from catalog; super-admin only |
| GET `/api/v1/admin/permissions` or `/grouped` | `permissions:view` | Read stable permission directory |
| GET `/api/v1/admin/permissions/effective` | authenticated Session | Current actor's effective codes and super-admin flag |

Responses use the common envelope. User lists return `items,total,page,page_size`.
System and super-admin roles are immutable. Only super-admins can assign system
roles or role permissions. Non-super administrators cannot alter their own roles
and can assign only roles whose permissions they already hold. Removing the last
active super-admin returns 409. Deleting a used role returns 409. The controlled
permission directory cannot be mutated through the API.

## Menus and navigation APIs

Every endpoint below requires a valid database-backed Session. Management reads
and writes additionally require the listed live permission. Mutations require
Origin and CSRF validation. `/menus/my` is an authenticated navigation projection,
not an authorization grant.

| Method/path | Required permission | Purpose |
| --- | --- | --- |
| GET `/api/v1/admin/menus/my` | authenticated Session | Current role-filtered, nested navigation tree |
| GET `/api/v1/admin/menus` or `/tree` | `menus:view` | Flat definitions / nested management tree |
| GET `/api/v1/admin/menus/:id` | `menus:view` | Menu definition and assigned role IDs |
| GET `/api/v1/admin/menus/roles` or `/permissions` | `menus:view` | Assignment and controlled catalog options |
| POST `/api/v1/admin/menus` | `menus:create` | Create directory, registered page or HTTPS external link |
| PUT `/api/v1/admin/menus/:id` | `menus:update` | Update menu definition |
| PUT `/api/v1/admin/menus/reorder` | `menus:update` | Atomically update sort order |
| PUT `/api/v1/admin/menus/:id/roles` | `menus:assign_roles` | Replace menu role assignments |
| DELETE `/api/v1/admin/menus/:id` | `menus:delete` | Delete a menu subject to hierarchy constraints |

Internal page route/component/permission combinations come from a fixed server
registry; clients cannot register arbitrary components or permissions. `my`
includes active/visible authorized pages and eligible ancestors, omits empty
directories, and gives super-admins all active/visible entries. The menu editor
uses these real endpoints and capability-gates controls, while every API request
still undergoes backend authorization.

## Dashboard APIs (Phase 3.1)

Each route below requires an active database Session and a live
`dashboard:view` permission check in Go. Responses use the standard
`code/message/data` envelope and `Cache-Control: no-store`. The same-origin proxy
allows these exact GET paths only, with no arbitrary nested route or write method.

| Method/path | Input | Data |
| --- | --- | --- |
| GET `/api/v1/admin/dashboard/summary` | none | `administrators`, `active_administrators`, `roles`, `active_roles`, `security_events`, `login_successes`, `login_failures` counts and `service: ready\|unavailable` |
| GET `/api/v1/admin/dashboard/activity` | optional `page=1..1000`, `page_size=1..50` (defaults 1/10) | `items` (`id`, `actor`, `action`, `target_type`, `created_at`), `total`, `page`, `page_size`; newest first |
| GET `/api/v1/admin/dashboard/trends` | required `range=7d\|30d` | `range`, `points` for operations and `login_successes`/`login_failures` UTC daily buckets including today |

Invalid range or pagination returns 400/40000; missing Session returns
401/40100, insufficient permission returns 403/40300, and database failures
return the generic 503/50301. The summary exposes only a safe readiness category,
never database host, credentials, environment values or raw errors. Activities
are existing security operation events; login aggregates come only from durable
`login_events`. Failures include rate-limited attempts.

## Audit and Login Logs APIs (Phase 3.4)

All four GET routes use the active database Session, live Go RBAC and no-store
responses. The Next same-origin proxy admits only the explicit list and UUID
detail paths. There are no public write/export routes.

| Method/path | Permission | Input / response |
| --- | --- | --- |
| GET `/api/v1/admin/audit-logs` | `audit:view` | `from`, `to` UTC dates (inclusive), `user_id` UUID, exact `action`, `page`, `page_size`; redacted `items,total,page,page_size` |
| GET `/api/v1/admin/audit-logs/:id` | `audit:view` | Redacted operation detail by UUID |
| GET `/api/v1/admin/login-logs` | `login_logs:view` | `from`, `to` UTC dates (inclusive), `user_id` UUID, `outcome=success\|failure\|rate_limited`, pagination |
| GET `/api/v1/admin/login-logs/:id` | `login_logs:view` | Classified login event detail by UUID |

Default window is the last 30 UTC calendar dates; maximum span 366 days,
`page=1..1000`, `page_size=1..50`, default 1/20. Sort is
`created_at DESC,id DESC`. Unknown/duplicate filters, invalid UUID/date and
out-of-bounds values return 400. No Session returns 401, missing live grant
403, missing detail 404, DB failure a generic 503. Existing operation events
have nullable request metadata; HTTP writes record only the Gin route template,
parsed IP and browser family. Unknown login names are not disclosed or stored.

## Profile APIs (Phase 3.5)

Profile APIs are authenticated self-service. The subject is always the user
from the live database Session; clients cannot submit a user ID. No additional
RBAC permission is required, so an authenticated administrator can access
their own Profile even without business-module grants. The Next proxy admits
only the paths and methods below. Every mutation requires the existing signed
CSRF cookie/header and valid same-origin request. Responses are `no-store`.

| Method/path | Input | Result |
| --- | --- | --- |
| GET `/api/v1/admin/profile` | none | `{user:{id,username,email,display_name}}` |
| PUT `/api/v1/admin/profile` | `{email,display_name}` | Updated safe user fields; email is case-insensitive unique |
| PUT `/api/v1/admin/profile/password` | `{current_password,new_password}` | Verifies current Argon2id password; on success bumps `auth_version`, revokes all sessions atomically and expires current cookies |
| GET `/api/v1/admin/profile/sessions` | `page=1..1000`, `page_size=1..50` (defaults 1/20) | Own active Sessions only: UUID, `current`, `created_at`, `last_seen_at`, `expires_at`, plus page totals |
| DELETE `/api/v1/admin/profile/sessions/:id` | UUID path | Revokes only an owned Session; revoking current expires its cookies and returns `current_revoked:true` |
| POST `/api/v1/admin/profile/sessions/revoke-others` | empty body | Revokes every other active Session and keeps current Session |

Profile updates are audited as `profile_updated`; password change as
`password_changed`; single/all-other revocations as `session_revoked` /
`sessions_revoked`, targeting only the current user. Writes share the existing
core -> user -> Session transaction lock order. Password changes recheck the
current password hash under transaction before commit. The persistent
`auth_login_limits` bucket limits reauthentication to five attempts per minute
per user without permanent account lockout. Password input and Session data are
never stored in browser storage; Session tokens and hashes are never returned.

Errors: 400 invalid/oversized input or pagination; 401 absent, expired, revoked,
disabled or stale Session; 403 CSRF/Origin rejection; 404 missing or foreign
Session (same response); 409 email already in use; 422 incorrect current
password or password policy failure; 429 reauthentication throttle with
`Retry-After`; 503 generic service/database failure. Response messages contain
no query details, password, hash or infrastructure error. The frontend
redirects to login after password change or current-Session revocation.

## System Settings APIs (Phase 3.2)

Both paths use the exact same-origin proxy path and an active database Session;
GET requires live `settings:view`, PUT requires live `settings:update`. Hidden
menus never authorize these routes. PUT also requires the existing Origin and
CSRF header/Cookie checks. The Go handler rechecks write permission inside the
transaction after locking the core metadata row. Updates are atomic with
`security_events` (`setting_updated`, target_type `setting`, target_id UUID).

| Method/path | Input | Response data |
| --- | --- | --- |
| GET `/api/v1/admin/settings` | none | `settings` object keyed by registered name, each `{value,sensitive,configured}`; defaults supplied if unset |
| PUT `/api/v1/admin/settings` | `{ "settings": { "site.name": "Example" } }`; nonempty subset of registered keys | Same redacted representation as GET; unregistered keys or invalid values are rejected atomically |

Allowed keys are `site.name`, `site.description`, `branding.logo_path`,
`localization.timezone`, `localization.language`, `general.default_page_size`.
Logo path is an empty string or a literal same-origin absolute path, with no
query/fragment, encoded characters, traversal, backslash or protocol-relative
URL. Language supports `en`, `zh-CN`, `zh-TW`; timezone is `UTC` or an IANA zone;
page size is 10/25/50/100. A future explicitly registered sensitive key has no
plaintext `value` on GET, only `sensitive: true, configured: true/false`.
No sensitive key is exposed in Phase 3.2. 400 means malformed JSON, 401 no
Session, 403 permission/CSRF failure, 422 unknown key/invalid value, 503
storage or encryption-key unavailable. Error envelopes never reveal keys or
database failures. Changes to language/page size are stored preferences and
do not retroactively change existing screens or pagination.

## Private Media APIs (Phase 3.3)

All endpoints use the existing database Session; each enforces its own live Go
permission. Writes also require Origin and CSRF. No public storage URL or
filesystem path is exposed. Metadata uses `code/message/data`; successful
content GET returns binary bytes directly. All responses are non-cacheable and
nosniff. The same-origin Next Node route admits only these paths/methods.

| Method/path | Permission | Input / result |
| --- | --- | --- |
| POST `/api/v1/admin/media` | `media:upload` | One multipart `file`; 26 MiB entire request, 25 MiB PDF/TXT or 10 MiB PNG/JPG/WebP; returns verified metadata |
| GET `/api/v1/admin/media` | `media:view` | `page=1..1000`, `page_size=1..100`, `type=image\|document`, `q` <=80 bytes; items/total/page/page_size |
| GET `/api/v1/admin/media/:id` | `media:view` | Ready metadata by UUID |
| GET `/api/v1/admin/media/:id/content` | `media:view` | Verified image inline; PDF/TXT always attachment; `?download=1` forces attachment; Range returns 416 |
| DELETE `/api/v1/admin/media/:id` | `media:delete` | Tombstone retained; Logo references return 409; missing/deleted returns 404 |

Errors use fixed public envelopes: 400 invalid, 401 unauthenticated, 403
permission/CSRF, 404 absent/non-ready, 409 referenced or concurrent, 413 too
large, 415 unsupported, 503 storage/verification unavailable. Content bytes
carry a controlled MIME, safe Content-Disposition, `nosniff`, no-store and
`default-src 'none'; sandbox` CSP; they never carry raw provider errors.

## Articles Demo APIs

Articles are a small example module, not a complete CMS. Each route requires a
live Session and the permission shown. Writes also require CSRF and Origin
validation; successful entity changes and audit events commit transactionally.

| Method/path | Permission | Purpose |
| --- | --- | --- |
| GET/POST `/api/v1/admin/articles` | `articles:view/create` | Search/filter/page or create a draft |
| GET/PUT/DELETE `/api/v1/admin/articles/:id` | `articles:view/update/delete` | Read or change an article |
| POST `/api/v1/admin/articles/:id/publish` or `/unpublish` | `articles:publish` | Publish or return to draft |
| GET/POST `/api/v1/admin/article-categories` | `article_categories:view/create` | List or create categories |
| PUT/DELETE `/api/v1/admin/article-categories/:id` | `article_categories:update/delete` | Update or delete an unused category |

Only `draft` and `published` states exist. Slugs are normalized and protected by
PostgreSQL uniqueness; collisions return 409. Referenced categories cannot be
deleted. Published articles have `published_at`; drafts do not. DTOs omit
passwords, hashes and internal database fields.

## Notifications, API Keys and Jobs

| Method/path | Permission | Purpose |
| --- | --- | --- |
| GET/POST `/api/v1/admin/notifications` | `notifications:view/create` | Read the current recipient's inbox or create a notification |
| POST `/api/v1/admin/notifications/:id/read` | `notifications:view` | Mark an owned notification as read |
| GET/POST `/api/v1/admin/api-keys` | `api_keys:view/create` | List owned metadata or issue a scoped Dashboard key |
| POST `/api/v1/admin/api-keys/:id/revoke` | `api_keys:revoke` | Revoke an owned key immediately |
| GET `/api/v1/admin/jobs` or `/:id` | `jobs:view` | List jobs or read bounded execution history |
| POST `/api/v1/admin/jobs` | `jobs:create` | Enqueue an allowlisted registered job kind |
| POST `/api/v1/admin/jobs/:id/retry` | `jobs:retry` | Retry an eligible failed job |

Raw API keys are returned only once; PostgreSQL stores only SHA-256 hashes.
Keys are limited to the read-only Dashboard API and cannot use browser Session
routes. Jobs use registered task kinds and bounded attempts; arbitrary shell
commands or client-selected code are not executable payloads. These routes use
live RBAC and the common CSRF/Origin protection for state-changing requests.

## Local Plugin APIs

All paths use the same-origin allowlisted Next.js Node proxy and Go's live
Session/RBAC checks. Mutations also require the existing Origin and CSRF checks.
There is no API to upload executable packages: installation selects a package
already present under the server's local trusted package root and verified
against an Ed25519 public key.

| Method/path | Permission | Input / result |
| --- | --- | --- |
| GET `/api/v1/admin/plugins` | `plugins:view` | Installed metadata, manifests, status and safe failure codes |
| GET `/api/v1/admin/plugins/packages` | `plugins:view` | Only local packages with valid signature and migration hashes |
| GET `/api/v1/admin/plugins/events?limit=1..100` | `plugins:view` | Recent lifecycle/configuration events, optionally filtered by plugin ID |
| POST `/api/v1/admin/plugins/install` | `plugins:install` | `{id,version}`; installs verified package migrations atomically |
| POST `/api/v1/admin/plugins/:id/enable` | `plugins:enable` | Requires enabled dependencies; activates plugin menus |
| POST `/api/v1/admin/plugins/:id/disable` | `plugins:disable` | Refused while enabled dependents exist; hides plugin menus |
| POST `/api/v1/admin/plugins/:id/upgrade` | `plugins:migrate` | Applies newer verified migrations; plugin must be disabled |
| POST `/api/v1/admin/plugins/:id/uninstall` | `plugins:uninstall` | Disabled plugin only; does not rollback plugin DDL; event history retained |
| GET/PUT `/api/v1/admin/plugins/:id/config` | `plugins:view` / `plugins:configure` | Typed declared settings; secret values are write-only and redacted on reads |
| GET/POST/PUT/DELETE `/api/v1/admin/plugins/:id/routes/:key` | Manifest-declared permission | Dispatches only to a build-time registered provider |

Errors use fixed envelopes: 400 malformed input, 401 no Session, 403 missing
permission or CSRF/Origin rejection, 404 unknown plugin/version/route, 409
dependency/state conflict, and 503 database/provider unavailable. SQL, signatures,
secrets and infrastructure errors are not returned. Plugin routes accept bounded
JSON only.
