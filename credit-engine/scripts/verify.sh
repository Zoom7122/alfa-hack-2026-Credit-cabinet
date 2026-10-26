#!/bin/sh
# Linux/Docker acceptance. The default exit path removes this run's stack.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
test "$(basename "$root")" = credit-engine
export CREDIT_ENGINE_TEST_LABEL="${CREDIT_ENGINE_TEST_LABEL:-ce-audit-$(date -u +%Y%m%d%H%M%S)-$$}"
export CREDIT_ENGINE_TEST_SOURCE="${CREDIT_ENGINE_TEST_SOURCE:-$root}"
relative=".work/re-audit/$CREDIT_ENGINE_TEST_LABEL"
export CREDIT_ENGINE_TEST_OUTPUT="/workspace/credit-engine/$relative"
export CREDIT_ENGINE_TEST_WORK_HOST="${CREDIT_ENGINE_TEST_WORK_HOST:-$CREDIT_ENGINE_TEST_SOURCE/$relative}"
export CREDIT_ENGINE_TEST_IMAGE="${CREDIT_ENGINE_TEST_IMAGE:-credit-engine:acceptance}"
mkdir -p "$relative" .work/tmp-linux .work/tmp-browser .work/browser-home .work/playwright .work/npm-cache
compose() { docker compose -p "$CREDIT_ENGINE_TEST_LABEL" -f compose.acceptance.yaml "$@"; }
reset_test_runtime() {
  compose exec -T postgres psql -v ON_ERROR_STOP=1 -U credit_test -d credit_engine_admin_test -c 'TRUNCATE credit_engine_runtime.bank_loans, credit_engine_runtime.bank_operations, credit_engine_runtime.bank_outbox, credit_engine_runtime.bank_state, credit_engine_runtime.scoring_runs CASCADE' > /dev/null
}
cleanup() {
  result=$?
  trap - EXIT INT TERM
  compose --profile checks logs --no-color > "$relative/containers.log" 2>&1 || true
  if [ "${KEEP_TEST_STACK:-0}" != 1 ]; then
    compose --profile checks down --remove-orphans --volumes > "$relative/cleanup.log" 2>&1 || result=1
    remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$CREDIT_ENGINE_TEST_LABEL") || result=1
    networks=$(docker network ls -q --filter "label=com.docker.compose.project=$CREDIT_ENGINE_TEST_LABEL") || result=1
    if [ -n "$remaining$networks" ]; then
      echo "Test project still has containers or networks" >> "$relative/cleanup.log"
      result=1
    fi
  fi
  echo "Acceptance artifacts: $root/$relative"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose config -q
compose build admin-service runtime-migrate > "$relative/docker-build.log" 2>&1
compose run --rm --no-deps node scripts/scenarios.mjs prepare default
compose up -d postgres runtime-migrate admin-migrate admin-service fixtures bank-state-service scoring-service
compose run --rm --no-deps go
compose run --rm --no-deps --entrypoint sh node -c 'if [ ! -f .work/playwright/node_modules/playwright/package.json ]; then npm install --prefix .work/playwright --cache .work/npm-cache --ignore-scripts --no-audit --no-fund playwright@1.63.0; fi'
compose run --rm --no-deps node scripts/javascript.mjs
compose run --rm --no-deps browser internal/adminui/testdata/ui_workflow.mjs
compose run --rm --no-deps node scripts/scenarios.mjs list > "$relative/scenarios.txt"
while IFS= read -r scenario <&3; do
  compose run --rm --no-deps node scripts/scenarios.mjs prepare "$scenario"
  reset_test_runtime
  compose up -d --no-deps --force-recreate bank-state-service
  compose run --rm --no-deps node scripts/scenarios.mjs run "$scenario"
done 3< "$relative/scenarios.txt"
compose run --rm --no-deps node scripts/scenarios.mjs prepare bank:bank_fixed_deal.json
reset_test_runtime
compose up -d --no-deps --force-recreate bank-state-service
compose stop admin-service
compose run --rm --no-deps node scripts/scenarios.mjs pending
compose up -d admin-service
compose run --rm --no-deps node scripts/scenarios.mjs retry
compose run --rm --no-deps browser internal/adminui/testdata/admin_acceptance.mjs
compose run --rm --no-deps node scripts/scenarios.mjs prepare default
reset_test_runtime
compose up -d --no-deps --force-recreate bank-state-service
compose run --rm --no-deps browser internal/adminui/testdata/runtime_smoke.mjs
compose run --rm --no-deps browser internal/adminui/testdata/live_activation.mjs
compose restart scoring-service
compose run --rm --no-deps browser internal/adminui/testdata/live_activation.mjs restart
compose run --rm --no-deps --entrypoint sh go scripts/rebuild.sh
reset_test_runtime
compose --profile checks up -d --no-deps bank-rebuilt scoring-rebuilt
compose run --rm --no-deps -e CREDIT_ENGINE_REBUILT=1 -e CREDIT_ENGINE_SCORING_URL=http://scoring-rebuilt:8082 -e CREDIT_ENGINE_BANK_URL=http://bank-rebuilt:8083 browser internal/adminui/testdata/runtime_smoke.mjs
printf '%s' fictional-acceptance-password | compose run --rm --no-deps -T --entrypoint /admin-password admin-migrate > "$relative/password-hash.txt"
compose run --rm --no-deps node scripts/report.mjs
