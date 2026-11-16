.PHONY: setup install dev up down logs migrate test integration smoke resilience build check e2e-phase21 e2e-phase22 e2e-phase23 e2e-phase24 e2e-phase31 e2e-phase32 e2e-phase35 e2e-phase36 e2e-starter admin-create menus-init
setup:
	node scripts/setup.mjs
install:
	cd backend && go mod download
	cd frontend && npm ci
dev: up
up: setup
	docker compose up -d --build --wait
down:
	docker compose down
logs:
	docker compose logs -f --tail=100
migrate:
	docker compose run --rm migrate up
test:
	if [ -d cmd/go-admin ]; then go test -race -count=1 ./cmd/go-admin && go vet ./cmd/go-admin; fi
	$(MAKE) -C backend test
	cd frontend && npm run lint && npm run typecheck && npm test
integration:
	cd backend && RUN_DB_TESTS=1 go test -count=1 -v ./tests
smoke:
	node scripts/smoke.mjs
resilience:
	node scripts/verify-resilience.mjs
build:
	if [ -d cmd/go-admin ]; then go build -o /tmp/goadmin-kit-go-admin ./cmd/go-admin; fi
	$(MAKE) -C backend build
	cd frontend && npm run build
check: test build smoke
e2e-phase21:
	node scripts/verify-phase21.mjs
e2e-phase22:
	E2E_AUTH=1 node scripts/verify-phase21.mjs
e2e-phase23:
	E2E_AUTH=1 E2E_RBAC=1 node scripts/verify-phase21.mjs
e2e-phase24:
	E2E_AUTH=1 E2E_RBAC=1 E2E_MENUS=1 node scripts/verify-phase21.mjs
e2e-phase31:
	E2E_AUTH=1 E2E_RBAC=1 E2E_MENUS=1 E2E_DASHBOARD=1 node scripts/verify-phase21.mjs
e2e-phase32:
	E2E_AUTH=1 E2E_RBAC=1 E2E_MENUS=1 E2E_DASHBOARD=1 E2E_SETTINGS=1 node scripts/verify-phase21.mjs
e2e-phase33:
	E2E_AUTH=1 E2E_RBAC=1 E2E_MENUS=1 E2E_DASHBOARD=1 E2E_SETTINGS=1 E2E_MEDIA=1 node scripts/verify-phase21.mjs
e2e-phase33-http:
	E2E_AUTH=1 E2E_MEDIA=1 E2E_MEDIA_HTTP=1 node scripts/verify-phase21.mjs
e2e-phase34:
	E2E_AUTH=1 E2E_RBAC=1 E2E_MENUS=1 E2E_DASHBOARD=1 E2E_SETTINGS=1 E2E_LOGS=1 node scripts/verify-phase21.mjs
e2e-phase35:
	E2E_PROFILE=1 node scripts/verify-phase21.mjs
e2e-phase36:
	E2E_ARTICLES=1 node scripts/verify-phase21.mjs
e2e-starter:
	E2E_STARTER=1 node scripts/verify-phase21.mjs
admin-create:
	cd backend && go run ./cmd/admin create
menus-init:
	cd backend && go run ./cmd/admin menus-init
media-bind:
	cd backend && go run ./cmd/admin media-bind
media-reconcile:
	cd backend && go run ./cmd/admin media-reconcile
media-retry:
	cd backend && go run ./cmd/admin media-retry
