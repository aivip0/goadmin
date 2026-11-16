# Module development boundary

The starter includes Auth, IAM, Menus, Dashboard, Settings, Media, Logs, Profile,
Articles, Notifications, API Keys, Jobs and trusted local Plugins. Articles is a
small complete business example, not a full CMS.

Backend modules belong under backend/internal/modules/<module>, with separate
handler, service, repository, model, dto, routes and permissions files following
the existing modules. Handlers validate input and map HTTP; services own business
rules; repositories own persistence. Register routes through the router and
providers; do not add business logic to cmd/api/main.go.

Schema changes require numbered SQL up/down migrations, matching API contracts
and focused tests. Do not use AutoMigrate. UUID IDs and UTC timestamp conventions
apply. Readiness derives the latest embedded migration version automatically.
Never edit released Migrations 1–13. Successful mutations and audit events should
commit in the same transaction. Reuse Session, live RBAC, CSRF and Origin guards.

Future frontend feature code belongs in features/<module>; app routes compose
features, services handle HTTP, types hold contracts. Reuse the UI tokens and
Button primitive. Extend the same-origin proxy allowlist explicitly. Register
controlled component keys; never turn a database key into an import path.
Test permissions and real desktop/mobile workflows against isolated PostgreSQL.
Business-specific extensions may use trusted build-time plugin Providers and
signed local manifests; uploaded binaries and scripts are never executable.
