# Development

Start with ARCHITECTURE.md and API.md. Implemented modules include administrator
authentication, RBAC, menus, dashboard, settings, Media, Audit/Login Logs and
self-service Profile, Articles, notifications, API keys, jobs and local plugins.
Reuse these boundaries rather than introducing a second authentication or UI stack.

## Environment

Use Go 1.26.x, Node.js 24.x, npm and Docker Compose v2. Backend dependencies are
locked in `backend/go.mod` and `backend/go.sum`; frontend dependencies are locked
in `frontend/package-lock.json`. Run `make setup`, then `make install`.
The single root `.env` is used by Compose, Go commands (root or backend cwd) and
the frontend dev/start wrapper. Process environment takes precedence.

| Variable | Default / meaning |
| --- | --- |
| APP_ENV | development; test and production supported |
| APP_NAME / LOG_LEVEL | GoAdmin Kit / info |
| API_HOST / API_PORT | 127.0.0.1 / 8080 locally; Compose binds container 0.0.0.0:8080 |
| FRONTEND_PORT | 3000 host port |
| POSTGRES_HOST / POSTGRES_PORT | 127.0.0.1 / 15432 locally; postgres:5432 in Compose |
| POSTGRES_USER / POSTGRES_DB | goadmin / goadmin |
| POSTGRES_PASSWORD | required; setup generates random local value |
| POSTGRES_SSLMODE | disable locally; production must enable TLS |
| DB_MAX_OPEN_CONNS / DB_MAX_IDLE_CONNS | 10 / 5; idle cannot exceed open |
| DB_CONNECT_TIMEOUT / HEALTH_TIMEOUT | 5s / 2s; 1ms..1m accepted |
| API_INTERNAL_URL | http://127.0.0.1:8080 locally; http://api:8080 in Compose |
| PUBLIC_ORIGINS | exact comma-separated browser origins; localhost:3000 and 127.0.0.1:3000 by default |
| TRUSTED_PROXIES | empty by default; explicit proxy IP/CIDR only, never /0 |
| COOKIE_SECURE / COOKIE_SAME_SITE | production requires true / lax or strict (default lax) |
| SESSION_TTL / CSRF_TTL | 12h / 1h; validated bounds |
| SESSION_IDLE_TTL | 30m; at least 1m and no longer than SESSION_TTL |
| CSRF_SECRET | production requires at least 32 bytes; empty development value generates an ephemeral process secret |
| MEDIA_STORAGE_ROOT | Empty disables Media; set an absolute, private, persistent directory outside web roots only after an approved schema upgrade |
| MEDIA_MIN_FREE_BYTES | Minimum free disk space reserve; upload also requires headroom for staging, incoming, and verification snapshot |

Do not add database secrets to NEXT_PUBLIC_* or commit `.env`. Compose passes
only API_INTERNAL_URL to the frontend. `make setup` preserves existing values.

## Commands

Keep test databases separate from any existing development or production data.
Migrations 1–13 are the template baseline; initialize a fresh database explicitly.
Existing databases require exact source/target confirmations and a restorable
backup. Old schemas intentionally report not-ready.

`make dev` or `make up`: build/start full stack. `make down`: stop/remove project
containers without removing persistent volume. `make logs`: bounded log stream.
See README for separate local API/Next processes and port-conflict handling.

`make test`: Go race tests and vet; frontend lint, strict types and unit tests.
`make integration`: explicit real DB tests (skipped by normal go test).
`make smoke`: running stack HTTP checks. `npm run test:e2e` in frontend: desktop
and mobile Chrome, screenshots, theme, refresh, error/recovery and JS errors.
Playwright uses an installed Google Chrome. Override E2E_BASE_URL if the host
frontend port differs from 3000. Browser fault simulation is separate from the
actual database outage/recovery acceptance check.

`make e2e-phase21` (after `make build`) creates a cryptographically named test
database in the configured PostgreSQL container, starts separate built API/Next
processes on free loopback ports, runs browser tests including real CSRF flows,
then stops only its processes and drops only its test database. For Phase 3
verification, set `E2E_DB_CONTAINER`, `POSTGRES_HOST`, `POSTGRES_PORT`,
`POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB` to a disposable PostgreSQL
container and set `E2E_DB_ISOLATED=1`; that container must carry the label
`goadmin.test.isolated=true`. Do not use a shared development service.
`make integration` uses a
random schema and checks migration upgrade/repeat/dirty/version mismatch and
transaction lock behavior; pass its connection variables explicitly too.

`make e2e-phase22` (after `make build`) adds randomly generated dedicated test
credentials and a one-time admin initialization in the isolated test database.
It verifies real Go/API/Next/browser login on desktop and mobile without printing
or retaining credentials. Only its own processes and test database are removed.

`make e2e-phase23` extends that isolated real-stack browser run with user and
role management, permission enforcement/revocation and last-super-admin checks.

`make e2e-phase35` runs the real Profile workflow on desktop and mobile against
the isolated PostgreSQL/Go/Next stack. It verifies own profile editing,
HttpOnly Session behavior, CSRF, current-password failure/success, session
ownership/revocation and immediate invalidation after password change. It
initializes a temporary administrator inside the random test database and
removes that database afterward. It does not alter a shared development stack.
Profile does not add a migration.

`make e2e-phase33` extends the isolated browser run through Media: the runner
creates a random database, a private temporary root, initializes a test admin,
binds that root to that database, starts isolated API/Next processes, then
removes only its own resources. Point `E2E_DB_CONTAINER`, `POSTGRES_HOST/PORT`,
`POSTGRES_USER/DB/PASSWORD` at a disposable PostgreSQL instance. Do not target
the persistent database. The isolated integration suite uses `make integration`
with the same disposable PostgreSQL environment.

To deploy on an explicitly approved, backed-up database, first migrate that
database to the current v13 baseline, provision the private `media_data` volume on the API only, and
set `MEDIA_STORAGE_ROOT` to its mount. With an empty new root, explicitly run
`MEDIA_BIND_CONFIRM_DATABASE=<exact POSTGRES_DB> make media-bind`; this does not
run a migration. An existing root/DB binding must match exactly: do not recreate
its marker. Run `make media-reconcile` on that same configured root for bounded
recovery. Never configure separate local roots for API replicas sharing one DB.
Keep a paired PostgreSQL dump and volume snapshot from a quiescent window;
restore the DB and volume together and allow startup to reverify each ready
object before serving Media. Missing or corrupt objects remain unavailable.
The CLI does not automate paired backups.

First admin on a separately approved, backed-up migrated database: supply
ADMIN_INIT_DATABASE (must match POSTGRES_DB), ADMIN_INIT_USERNAME,
ADMIN_INIT_EMAIL, ADMIN_INIT_PASSWORD and optional ADMIN_INIT_DISPLAY_NAME through
secure environment injection. Invoke `make admin-create` or
`cd backend && go run ./cmd/admin create`. Do not write secrets in shell history,
command arguments, shared logs or source. Passwords require 15+ characters and
at most 128 bytes. The CLI prints its DB target and refuses stale schemas,
existing users and repeated initialization; it never migrates the database.

`make resilience`: explicitly stop/start only this Compose project's PostgreSQL,
assert HTTP 503/200 liveness separation and browser error/recovery, and verify
the foundation UUID persists. Run only against a disposable local development
stack, not production or a shared database. Screenshots are in
`frontend/test-results/resilience/`. The script restores PostgreSQL in `finally`.

The Next ESLint plugins currently require ESLint 9; ESLint 10 yields invalid peer
dependencies. ESLint 9 is pinned for compatibility despite its upstream support
warning. The current npm audit reports seven moderate development-tool dependency
advisories (TypeScript ESLint / minimatch); production dependency audit is clean.
Do not force incompatible major upgrades. Recheck this toolchain before release.

Go emits JSON slog records to stdout. Request logs include path, status,
duration and generated ID, not request bodies, query strings or credentials.
HTTP server has bounded read/write/header/idle timeouts and graceful SIGTERM.

### Local Plugins

Server-only variables `PLUGIN_PACKAGE_ROOT`, `PLUGIN_TRUSTED_PUBLIC_KEYS` and
`PLUGIN_CONFIG_ENCRYPTION_KEY` configure the optional local plugin system. The
root contains `<root>/<plugin-id>/<semver>/manifest.json`, `manifest.sig`, and
optional `migrations/*.sql`. The raw manifest must be signed by a configured
base64 Ed25519 public key; each SQL migration digest must match its signed
manifest. Do not accept unreviewed content in this directory. The system does
not execute package binaries, scripts or JavaScript. Runtime routes and lifecycle
preflight hooks require trusted build-time Go registration. Secret plugin config
requires a stable, separately backed-up 32-byte encryption key; losing it makes
those secrets unrecoverable. Signed SQL is trusted publisher input, not a DB
sandbox, so review migrations before trusting a publisher key. Plugin migrations
on existing installations remain explicit, backed-up maintenance operations.

Production Next builds explicitly use the stable Webpack option because this
machine restricts the local port binding used by Turbopack compilation. Dev
mode still uses Next's default compiler. No experimental Next flags are enabled.
