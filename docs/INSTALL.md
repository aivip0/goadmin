# Installation

GoAdmin Kit can be used as a repository or generated as an independent starter
project with the `go-admin` CLI.

## CLI installation

```sh
go install github.com/aivip0/goadmin/cmd/go-admin@latest
go-admin version
go-admin new my-admin --version v0.1.1
cd my-admin
go-admin doctor
go-admin init
docker compose up -d postgres --wait
go-admin init --skip-install --database my_admin
```

The default resolves the latest stable GitHub Release. `--version` pins an exact
tag; there is no fallback to the moving default branch. `--module` sets the Go
module, and `--name` sets a safe ASCII display name. The generator
refuses to overwrite a target directory and creates fresh secrets in `.env`
with mode `0600`. The generated project is independent of the source checkout.

## Source checkout

```sh
git clone https://github.com/aivip0/goadmin.git
cd goadmin
make setup
make install
```

## Database initialization

For a fresh local database, start only the PostgreSQL service and select the
target explicitly in the environment. The migration command uses the embedded
SQL files and never silently chooses another database. Create the first admin
with `ADMIN_INIT_DATABASE`, `ADMIN_INIT_USERNAME`, `ADMIN_INIT_EMAIL` and
`ADMIN_INIT_PASSWORD` supplied through a secure environment mechanism. Passwords
are not accepted as command-line arguments or written to logs.

Existing-schema upgrades require the maintenance confirmation variables and a
restorable database and Media volume backup. Do not run an upgrade against a
database unless its identity and observed schema version have been verified.

`init` preserves an existing `.env`. Without `--database`, it installs backend
and frontend dependencies without connecting to PostgreSQL. With `--database`,
the name must exactly match effective `POSTGRES_DB` (process environment takes
precedence over `.env`) before invoking the guarded embedded migrator. Existing
database upgrades retain all backup and exact-version confirmation requirements.
Use `--skip-install` after dependencies have already been installed.

`doctor` checks Go >=1.26, Node.js 24, npm >=10, required project files, database
configuration, ports, CSRF and independent encryption keys. It never contacts or
changes a database. Docker is needed only for the optional Compose workflow.

For offline/local development of the generator:

```sh
go build -o /tmp/go-admin ./cmd/go-admin
cd /tmp
/tmp/go-admin new local-test --source-dir /absolute/path/to/goadmin --version v0.1.1
```
