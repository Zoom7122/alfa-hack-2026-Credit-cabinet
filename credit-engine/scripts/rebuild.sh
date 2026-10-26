#!/bin/sh
set -eu
: "${CREDIT_ENGINE_UI_OUTPUT:?isolated output required}"
rebuild_root="$CREDIT_ENGINE_UI_OUTPUT/rebuild-$(date -u +%Y%m%d%H%M%S)-$$/credit-engine"
go run scripts/rebuild.go -output "$rebuild_root" -archive "$CREDIT_ENGINE_UI_OUTPUT/accepted-release.zip"
cd "$rebuild_root"
go run ./cmd/modelgen check > "$CREDIT_ENGINE_UI_OUTPUT/rebuilt-modelgen.json"
CGO_ENABLED=0 go build -o "$CREDIT_ENGINE_UI_OUTPUT/scoring-rebuilt" ./cmd/scoring-service
