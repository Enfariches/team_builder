SHELL := /bin/bash
GOBIN ?= $(shell go env GOPATH)/bin

.PHONY: up down migrate tools gen test build

up:
	docker compose up --build -d

down:
	docker compose down

migrate:
	docker compose run --rm migrate

# Ставит кодогенераторы (sqlc собирается с cgo, нужен C-компилятор)
tools:
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0

# Сгенерированный код не коммитится, поэтому gen нужен перед сборкой и тестами
gen:
	cd backend && $(GOBIN)/oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
	cd backend && $(GOBIN)/sqlc generate

test: gen
	go test ./backend/...

build: gen
	go build ./...
