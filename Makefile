.DEFAULT_GOAL := help
.PHONY: help start stop reset logs test test-integration lint

# Optional settings come from .env (see .env.example); these are the defaults.
-include .env
DB_PORT           ?= 5432
API_PORT          ?= 8080
POSTGRES_USER     ?= pokedex
POSTGRES_PASSWORD ?= pokedex
POSTGRES_DB       ?= pokedex
DATABASE_URL      ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(DB_PORT)/$(POSTGRES_DB)?sslmode=disable
export DB_PORT API_PORT POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB DATABASE_URL

help:             ## Show this help
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-17s %s\n", $$1, $$2}'

start:            ## Start db + seed + API in Docker, in the background (data is kept)
	docker compose up --build -d --wait api
	@echo "API running on http://localhost:$(API_PORT)"

stop:             ## Stop everything (data is kept)
	docker compose down

reset:            ## Delete ALL data (including favorites) and start clean
	docker compose down -v
	$(MAKE) start

logs:             ## Follow the API logs
	docker compose logs -f api

test:             ## Unit tests (no database needed)
	go test ./...

test-integration: ## Unit + PostgreSQL tests (starts the db for you)
	docker compose up -d --wait db
	TEST_DATABASE_URL=$(DATABASE_URL) go test -count=1 ./...

lint:             ## gofmt + go vet
	gofmt -l .
	go vet ./...
