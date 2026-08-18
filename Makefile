DATABASE_URL ?= postgres://notes_user:notes_password@localhost:5432/notes_db?sslmode=disable
MIGRATIONS_PATH ?= db/migrations
MIGRATE ?= go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@latest
DEV_AUTH_DIR ?= .dev/auth
DEV_JWT_ISSUER ?= notes-dev
DEV_JWT_AUDIENCE ?= notes-api
DEV_JWT_SUBJECT ?=
DEV_JWT_DAYS ?= 365
DEV_JWKS_ADDR ?= :8081

.PHONY: db-up db-down db-reset migrate-up migrate-down migrate-version migrate-force dev-auth dev-jwks dev-auth-verify sqlc run-api run-worker test test-integration lint tidy

db-up:
	docker compose up -d 

db-down:
	docker compose down

db-reset:
	@echo "WARNING: this deletes the local PostgreSQL volume and all local notes data."
	@powershell -NoProfile -Command "$$answer = Read-Host 'Type DELETE to continue'; if ($$answer -ne 'DELETE') { exit 1 }"
	docker compose down -v
	docker compose up -d postgres

migrate-up:
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DATABASE_URL)" down 1

migrate-version:
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DATABASE_URL)" version

migrate-force:
ifndef VERSION
	$(error VERSION is required, for example: make migrate-force VERSION=1)
endif
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DATABASE_URL)" force $(VERSION)

dev-auth:
	go run ./cmd/devauth generate -out "$(DEV_AUTH_DIR)" -issuer "$(DEV_JWT_ISSUER)" -audience "$(DEV_JWT_AUDIENCE)" -subject "$(DEV_JWT_SUBJECT)" -days $(DEV_JWT_DAYS)

dev-jwks:
	go run ./cmd/devauth serve -dir "$(DEV_AUTH_DIR)" -addr "$(DEV_JWKS_ADDR)"

dev-auth-verify:
	go run ./cmd/devauth verify -dir "$(DEV_AUTH_DIR)" -issuer "$(DEV_JWT_ISSUER)" -audience "$(DEV_JWT_AUDIENCE)"

sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

test:
	go test ./...

test-integration:
	go test -tags=integration ./tests/integration/...

lint:
	go vet ./...

tidy:
	go mod tidy
