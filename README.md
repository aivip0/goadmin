# GoAdmin Kit

GoAdmin Kit is a reusable, production-oriented admin starter template built with
Go, Gin, PostgreSQL, Next.js App Router, TypeScript, Tailwind CSS and shadcn/ui.
It provides authentication, PostgreSQL sessions, RBAC, dynamic menus, settings,
media, audit and login logs, profile management, articles demo functionality,
notifications, API keys, jobs and a trusted local plugin system.

The repository is intentionally business-neutral. Applications such as content
platforms, SaaS products and SEO tools should add domain behavior through modules
or trusted build-time plugins rather than changing the core admin foundation.

## Requirements

- Go 1.26+
- Node.js 24 and npm
- Docker Engine with Compose v2
- PostgreSQL 17 for local development (Compose supplies it)

## Create a new project

Install the CLI from a tagged release:

```sh
go install github.com/aivip0/goadmin/cmd/go-admin@latest
go-admin new my-admin --version v0.1.2
cd my-admin
go-admin doctor
go-admin init
docker compose up -d postgres --wait
go-admin init --skip-install --database my_admin
```

`go-admin new` uses an exact GitHub Release and refuses to overwrite an existing
directory. It creates a new `.env` with fresh local secrets, rewrites the Go
module and frontend package name, and excludes private development notes and
business-specific files. `go-admin init` installs dependencies but never chooses
or migrates a database implicitly.

## Run locally

For the repository itself:

```sh
make setup
make install
docker compose up -d postgres --wait
```

Initialize the fresh database explicitly before creating an administrator:

```sh
go run ./cmd/go-admin init --skip-install --database goadmin
```

Create the first administrator using secure environment injection. For Bash:

```sh
read -r -p 'Username: ' ADMIN_INIT_USERNAME
read -r -p 'Email: ' ADMIN_INIT_EMAIL
read -r -s -p 'Password: ' ADMIN_INIT_PASSWORD; printf '\n'
export ADMIN_INIT_USERNAME ADMIN_INIT_EMAIL ADMIN_INIT_PASSWORD
export ADMIN_INIT_DATABASE=goadmin # generated projects: use their POSTGRES_DB
make admin-create
unset ADMIN_INIT_PASSWORD
```

Then start the API and frontend in separate terminals:

```sh
make -C backend dev
cd frontend && npm run dev
```

Open <http://localhost:3000>. API readiness is available at
<http://localhost:8080/api/v1/health>.

## Architecture

The backend uses Handler → Service → Repository boundaries and versioned SQL
migrations. The frontend uses Next.js App Router and a same-origin proxy; it
never connects directly to PostgreSQL. Session tokens are HttpOnly cookies,
CSRF and Origin checks protect state-changing requests, and authorization is
enforced by Go handlers using effective RBAC permissions.

Migrations 1–13 are the current template baseline. Existing-schema upgrades are
explicit maintenance operations requiring a selected database, observed source
and target versions, and a restorable backup reference. A fresh project can be
initialized in an isolated PostgreSQL database; do not point development or
production commands at an unknown database.

## Plugin boundary

The plugin system supports trusted local signed packages, build-time Providers,
idempotent permissions and menu registration, plugin configuration and plugin
migrations. It does not provide an online marketplace and does not execute
uploaded Go binaries, shell commands or other untrusted code.

## Documentation

- [Installation](docs/INSTALL.md)
- [Quick start](docs/QUICKSTART.md)
- [Development](docs/DEVELOPMENT.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Database](docs/DATABASE.md)
- [API](docs/API.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Module development](docs/MODULE_DEVELOPMENT.md)

## License

MIT. See [LICENSE](LICENSE).
