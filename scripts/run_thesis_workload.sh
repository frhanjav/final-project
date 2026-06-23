#!/usr/bin/env bash
set -euo pipefail

ENDPOINT="${ENDPOINT:-http://localhost:8080/invoke}"
HEALTH_ENDPOINT="${HEALTH_ENDPOINT:-http://localhost:8080/healthz}"
METRICS_FILE="${METRICS_FILE:-metrics.csv}"
SUMMARY_FILE="${SUMMARY_FILE:-loadtest_summary.csv}"
PYTHON_BIN="${PYTHON_BIN:-python3}"
EVICTION_WAIT_SECONDS="${EVICTION_WAIT_SECONDS:-35}"
HIGH_LOAD_RPS="${HIGH_LOAD_RPS:-1000}"
HIGH_LOAD_SECONDS="${HIGH_LOAD_SECONDS:-3}"
HIGH_LOAD_CONCURRENCY="${HIGH_LOAD_CONCURRENCY:-128}"

if ! curl -fsS "${HEALTH_ENDPOINT}" >/dev/null; then
  echo "server is not reachable at ${HEALTH_ENDPOINT}" >&2
  echo "start it first with: go run ." >&2
  exit 1
fi

mkdir -p "$(dirname "${SUMMARY_FILE}")"
rm -f "${SUMMARY_FILE}"

if [[ ! -f "${METRICS_FILE}" ]]; then
  echo "metrics file ${METRICS_FILE} does not exist yet" >&2
  echo "start the server with METRICS_FILE=${METRICS_FILE} go run . or send one request first" >&2
  exit 1
fi

go run ./cmd/loadtest -endpoint "${ENDPOINT}" -label cold-baseline -force-tier 0 -total 12 -rps 4 -concurrency 1 -duration-ms 250 -output "${SUMMARY_FILE}"
go run ./cmd/loadtest -endpoint "${ENDPOINT}" -label warm-runtime -force-tier 1 -total 12 -rps 4 -concurrency 1 -duration-ms 250 -output "${SUMMARY_FILE}"
go run ./cmd/loadtest -endpoint "${ENDPOINT}" -label hot-paused -force-tier 2 -total 12 -rps 4 -concurrency 1 -duration-ms 250 -output "${SUMMARY_FILE}"

go run ./cmd/loadtest -endpoint "${ENDPOINT}" -label auto-burst -total 36 -rps 18 -concurrency 12 -duration-ms 250 -output "${SUMMARY_FILE}"
sleep "${EVICTION_WAIT_SECONDS}"
go run ./cmd/loadtest -endpoint "${ENDPOINT}" -label post-eviction -total 8 -rps 4 -concurrency 2 -duration-ms 250 -output "${SUMMARY_FILE}"

go run ./cmd/loadtest -endpoint "${ENDPOINT}" -label high-concurrency -seconds "${HIGH_LOAD_SECONDS}" -rps "${HIGH_LOAD_RPS}" -concurrency "${HIGH_LOAD_CONCURRENCY}" -duration-ms 50 -output "${SUMMARY_FILE}"

"${PYTHON_BIN}" plot.py "${METRICS_FILE}" "${SUMMARY_FILE}"
