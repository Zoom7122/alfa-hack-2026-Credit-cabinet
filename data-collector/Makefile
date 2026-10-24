.PHONY: run run-publisher run-worker test test-integration lint swagger

# API; нужен DATABASE_URL (задачи на сбор и outbox — в Postgres)
run:
	go run ./cmd/data-collector

# outbox → Kafka; нужны DATABASE_URL и KAFKA_BROKERS (по умолчанию localhost:9094)
run-publisher:
	go run ./cmd/outbox-publisher

# Kafka → сбор со всех моков → raw_collections; нужны DATABASE_URL, KAFKA_BROKERS, MOCK_API_URL
run-worker:
	go run ./cmd/worker

test:
	go test ./...

# нужен запущенный mock-api (MOCK_API_URL, BANK_API_URL; по умолчанию localhost:8090);
# Postgres (схема из модуля db) — если задан DATABASE_URL, Kafka — если задан KAFKA_BROKERS
test-integration:
	go test -tags integration -count=1 ./...

lint:
	go vet ./...

# перегенерировать docs/ после изменения аннотаций над хендлерами
swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 fmt -g cmd/data-collector/main.go
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/data-collector/main.go -o docs --parseInternal --outputTypes go,json,yaml

# Install requirements.txt first; real XLS/PDF fixtures are supplied locally, never committed.
.PHONY: test-financial
test-financial:
	python3 -m unittest discover -s financial_parser/tests -v
	go test ./internal/financial
