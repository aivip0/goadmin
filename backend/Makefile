.PHONY: dev migrate test build
dev:
	go run ./cmd/api
migrate:
	go run ./cmd/migrate up
test:
	go test -race -count=1 ./...
	go vet ./...
build:
	go build -o bin/api ./cmd/api
	go build -o bin/migrate ./cmd/migrate
	go build -o bin/healthcheck ./cmd/healthcheck
	go build -o bin/admin ./cmd/admin
