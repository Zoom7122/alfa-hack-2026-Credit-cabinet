# Берёт POSTGRES_* из .env в корне репозитория (шаблон — .env.example).
-include ../.env

POSTGRES_HOST ?= localhost
POSTGRES_PORT ?= 5432
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
export DATABASE_URL

.PHONY: up down status new lint

up:
	go run ./cmd/migrate up

down:
	go run ./cmd/migrate down

status:
	go run ./cmd/migrate status

# make new name=add_questionnaires
new:
	go run github.com/pressly/goose/v3/cmd/goose@v3.29.0 -dir migrations -s create $(name) sql

lint:
	go vet ./...
