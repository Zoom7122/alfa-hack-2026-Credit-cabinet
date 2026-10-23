# Общие команды проекта. Настройки — в .env (шаблон — .env.example).
-include .env

DOZZLE_PORT ?= 8888
MODULES := data-collector db mock-api

.PHONY: help env up down restart build ps logs migrate migrate-status test test-integration clean

help: ## список команд
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

env: ## создать .env из шаблона, если его нет
	@test -f .env || (cp .env.example .env && echo ".env создан из .env.example")

up: env ## собрать и поднять все сервисы
	docker compose up -d --build
	@echo "Логи: http://localhost:$(DOZZLE_PORT)"

down: ## остановить сервисы (данные БД сохраняются)
	docker compose down

restart: down up ## перезапустить всё

build: ## собрать образы
	docker compose build

ps: ## статус контейнеров
	docker compose ps -a

logs: ## логи в терминале (s=имя сервиса для одного)
	docker compose logs -f $(s)

migrate: env ## накатить миграции
	docker compose run --rm migrate up

migrate-status: env ## статус миграций
	docker compose run --rm migrate status

test: ## тесты и vet всех Go-модулей
	@for m in $(MODULES); do echo "== $$m"; (cd $$m && go vet ./... && go test ./...) || exit 1; done

test-integration: ## интеграционные тесты data-collector против запущенных mock-api и Postgres (сначала make up)
	$(MAKE) -C data-collector test-integration \
		BANK_API_URL=http://localhost:$(or $(MOCK_API_PORT),8090)/bank/v1 \
		MOCK_API_URL=http://localhost:$(or $(MOCK_API_PORT),8090) \
		DATABASE_URL=postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(or $(POSTGRES_PORT),5432)/$(POSTGRES_DB)?sslmode=disable

clean: ## остановить и удалить данные БД
	docker compose down -v
