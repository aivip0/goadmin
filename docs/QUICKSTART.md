# Quick Start

```sh
go install github.com/aivip0/goadmin/cmd/go-admin@latest
go-admin new my-admin --version v0.1.0
cd my-admin
go-admin doctor
go-admin init
docker compose up -d postgres --wait
go-admin init --skip-install --database my_admin
```

In one terminal start the API:

```sh
make -C backend dev
```

In another terminal start Next.js:

```sh
cd frontend
npm run dev
```

Initialize one administrator with secure environment injection as shown in
the README (`ADMIN_INIT_DATABASE=my_admin` for this example), then open
<http://localhost:3000>. The admin UI uses same-origin browser requests and the
API stores sessions server-side in PostgreSQL.

The CLI runs migrations only when `init --database` names the exact configured
target. Repeating initialization preserves configuration and uses the migration
version table to avoid applying SQL again. There is no default admin/password.

Media starts disabled until its private root is explicitly bound. For a local
process, create a private absolute directory (`mkdir -m 700 /absolute/private/media`),
set `MEDIA_STORAGE_ROOT` in `.env`, then run `MEDIA_BIND_CONFIRM_DATABASE=my_admin
ADMIN_INIT_DATABASE=my_admin make media-bind` before starting the API. For Docker,
use the private `media_data` volume at `/var/lib/goadmin/media` and run binding as
the application's non-root user after migrations and volume initialization.
Never expose the storage directory as a web root; see `docs/DEPLOYMENT.md`.
