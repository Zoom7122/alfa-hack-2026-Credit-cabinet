.PHONY: run test test-integration lint swagger

run:
	go run ./cmd/data-collector

test:
	go test ./...

# нужен запущенный mock-api (MOCK_API_URL, BANK_API_URL; по умолчанию localhost:8090);
# запись в raw_collections проверяется, если задан DATABASE_URL (схема из модуля db)
test-integration:
	go test -tags integration -count=1 ./...

lint:
	go vet ./...

# перегенерировать docs/ после изменения аннотаций над хендлерами
swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 fmt -g cmd/data-collector/main.go
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/data-collector/main.go -o docs --parseInternal --outputTypes go,json,yaml
