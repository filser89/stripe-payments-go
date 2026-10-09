SHELL := /bin/bash
export GOTOOLCHAIN := go1.27.2
export PATH := $(CURDIR)/.tools/bin:$(PATH)

.PHONY: setup build up down logs migrate generate test verify
setup:
	@./scripts/setup.sh
build:
	@mkdir -p bin
	go build -trimpath -o bin/service ./cmd/service
up:
	@./scripts/up.sh
down:
	docker compose down
logs:
	docker compose logs --tail=100 -f app migrate
migrate:
	docker compose run --rm --build migrate
generate:
	./.tools/bin/sqlc generate
test:
	go test -count=1 -timeout=5m ./...
verify:
	@./scripts/verify.sh
