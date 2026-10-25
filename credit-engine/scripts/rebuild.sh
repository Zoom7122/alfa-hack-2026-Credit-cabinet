#!/bin/sh
set -eu
: "${CREDIT_ENGINE_UI_OUTPUT:?isolated output required}"
go run scripts/rebuild.go -output "$CREDIT_ENGINE_UI_OUTPUT/rebuild/credit-engine" -archive "$CREDIT_ENGINE_UI_OUTPUT/accepted-release.zip"
cd "$CREDIT_ENGINE_UI_OUTPUT/rebuild/credit-engine"
go run ./cmd/modelgen check > "$CREDIT_ENGINE_UI_OUTPUT/rebuilt-modelgen.json"
CGO_ENABLED=0 go build -o "$CREDIT_ENGINE_UI_OUTPUT/scoring-rebuilt" ./cmd/scoring-service
