.PHONY: run test test-integration lint swagger

run:
	go run ./cmd/data-collector

test:
	go test ./...

# нужен запущенный mock-api (BANK_API_URL, по умолчанию http://localhost:8090/bank/v1)
test-integration:
	go test -tags integration -count=1 ./...

lint:
	go vet ./...

# перегенерировать docs/ после изменения аннотаций над хендлерами
swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 fmt -g cmd/data-collector/main.go
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/data-collector/main.go -o docs --parseInternal --outputTypes go,json,yaml
