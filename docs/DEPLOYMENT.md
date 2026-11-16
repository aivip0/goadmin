# Deployment foundation

The commands below are for a fresh local stack or an explicitly approved
deployment. The baseline is Migration 1–13. Verify the database identity,
current version and paired backups before upgrading an existing installation.
Existing-schema upgrades require the migrator's exact confirmation values.

```sh
make setup
docker compose up -d --build --wait
docker compose ps -a
make smoke
```

The dependency chain is PostgreSQL healthy -> one-shot migrate exit 0 -> API
ready -> frontend. A migrator exit 0 is expected, not a failed service. Apps run
as non-root users. Containers restart unless stopped; one-shot migrations do not
restart. Docker JSON logs rotate at 10 MB with three retained files.

All published ports bind 127.0.0.1. The provided Compose stack is local development
infrastructure, not an Internet-ready production deployment. Production needs
TLS-terminating reverse proxy, PostgreSQL TLS/certificate configuration, managed
secrets, backups, image scanning and a reviewed network policy. `deploy/nginx`
is reserved; no production reverse proxy is supplied by this template.

Production must explicitly set COOKIE_SECURE=true, HTTPS PUBLIC_ORIGINS and a
managed CSRF_SECRET of at least 32 bytes, as well as PostgreSQL TLS. Compose's
local COOKIE_SECURE=false default is rejected by the API in production. Share
CSRF_SECRET across API replicas and never log it. Configure TRUSTED_PROXIES only
for controlled immediate proxies that sanitize incoming forwarding headers.

Build images from the root with deploy/docker Dockerfiles. Backend embeds SQL
and OpenAPI. Frontend uses standalone output and reads API_INTERNAL_URL at runtime,
so changing upstream does not require rebuilding browser assets. Docker context
excludes root environment files and local build/dependency directories.

Root .env host names/ports apply to local processes. Compose intentionally
overrides container host/ports; host port changes do not change internal ports.
Do not run host processes and Compose on the same ports simultaneously.

`docker compose restart postgres` is data-preserving. `docker compose down`
keeps the volume. Never use `down -v` unless you explicitly intend to remove the
database. Back up with PostgreSQL tools before schema changes. Do not print
`docker compose config` unredacted in shared logs because it expands secrets.

API health becomes 503 during a database outage while liveness remains 200.
Docker health states alone do not restart an unhealthy container; the running
API retries DB readiness on each request and recovers when PostgreSQL returns.
