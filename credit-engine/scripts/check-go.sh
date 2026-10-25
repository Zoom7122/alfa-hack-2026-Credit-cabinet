#!/bin/sh
set -eu
: "${CREDIT_ENGINE_UI_OUTPUT:?isolated output directory required}"
go version > "$CREDIT_ENGINE_UI_OUTPUT/go-version.txt"
go test -race -tags=integration -count=1 -shuffle=on -json ./... > "$CREDIT_ENGINE_UI_OUTPUT/go-tests.jsonl" 2> "$CREDIT_ENGINE_UI_OUTPUT/go-tests.stderr"
go vet ./... > "$CREDIT_ENGINE_UI_OUTPUT/vet.log" 2>&1
go build -trimpath -o "$CREDIT_ENGINE_UI_OUTPUT/bin/" ./cmd/... > "$CREDIT_ENGINE_UI_OUTPUT/build.log" 2>&1
go run ./cmd/modelgen validate > "$CREDIT_ENGINE_UI_OUTPUT/modelgen-validate.json"
go run ./cmd/modelgen check > "$CREDIT_ENGINE_UI_OUTPUT/modelgen-check.json"
