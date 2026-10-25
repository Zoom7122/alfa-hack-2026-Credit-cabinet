.PHONY: run run-publisher run-worker run-signature-worker sigdemo test test-integration lint swagger

# API; нужен DATABASE_URL (задачи на сбор и outbox — в Postgres)
run:
	go run ./cmd/data-collector

# outbox → Kafka; нужны DATABASE_URL и KAFKA_BROKERS (по умолчанию localhost:9094)
run-publisher:
	go run ./cmd/outbox-publisher

# Kafka → сбор внешних систем / обработка финансовых документов; нужны DATABASE_URL, KAFKA_BROKERS
run-worker:
	go run ./cmd/worker

# Kafka (data-collector.documents) → проверка ЭП → signature_reports; нужны DATABASE_URL, KAFKA_BROKERS,
# DOCUMENTS_DIR с файлами API; профиль TEST с тестовыми корнями из testdata
run-signature-worker:
	SIGNATURE_TRUST_DIR=$${SIGNATURE_TRUST_DIR:-testdata/signature/trust} go run ./cmd/signature-worker

# заново выпустить тестовый УЦ профиля TEST и подписать демо-файлы (examples/files): меняет корень и CRL
sigdemo:
	go run ./tools/sigdemo -trust testdata/signature/trust -examples examples/files

test:
	go test ./...

# нужен запущенный mock-api (MOCK_API_URL, BANK_API_URL; по умолчанию localhost:8090);
# Postgres (схема из модуля db) — если задан DATABASE_URL, Kafka — если задан KAFKA_BROKERS
test-integration:
	go test -tags integration -count=1 ./...

lint:
	go vet ./...

# перегенерировать docs/ после изменения аннотаций над хендлерами или примеров в examples/
swagger:
	go run ./tools/swaggerexamples -examples examples -out docs/markdown
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 fmt -g cmd/data-collector/main.go
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/data-collector/main.go -o docs --parseInternal --outputTypes go,json,yaml --md docs/markdown
