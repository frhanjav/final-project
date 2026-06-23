# Multi-Tiered Serverless Pre-warming Simulator

This project simulates a three-tier serverless container pool inspired by AFaaS-style multi-level readiness:

- Tier 0: cold start, create a fresh container for each request and remove it immediately.
- Tier 1: warm runtime, keep one running container alive and execute work through `docker exec`.
- Tier 2: hot paused, keep one container paused and unpause/exec/pause it per request.

The gateway exposes `POST /invoke`, logs per-request metrics to `metrics.csv`, and runs an eviction worker that scales warm tiers back to zero after idle periods.

## Project Files

- `main.go`: HTTP server bootstrap and graceful shutdown.
- `config.go`: environment-driven configuration.
- `gateway.go`: `/invoke` and `/healthz` handlers.
- `pool.go`: Docker tier management, request routing, warm-up, and eviction.
- `metrics.go`: CSV logging.
- `cmd/loadtest/main.go`: configurable load generator for repeatable experiments.
- `Dockerfile` and `task.py`: mock FaaS runtime image.
- `plot.py`: chart generation for your report.
- `scripts/run_thesis_workload.sh`: runs baseline, burst, eviction, and high-concurrency scenarios.

## Setup

1. Initialize the module dependencies:

```bash
go mod tidy
```

2. Build the mock container image:

```bash
docker build -t multitier-faas-mock:latest .
```

3. Start Docker Desktop or any Docker daemon accessible from your shell.

4. Run the simulator:

```bash
go run .
```

The server starts on `http://localhost:8080` by default.

## Environment Variables

- `PORT`: HTTP port, default `8080`
- `FAAS_IMAGE`: Docker image name, default `multitier-faas-mock:latest`
- `TASK_DURATION_MS`: simulated work duration, default `1000`
- `EVICTION_SECONDS`: idle timeout for Tier 1 and Tier 2 eviction, default `30`
- `EVICTION_SWEEP_SECONDS`: eviction scan interval, default `5`
- `REQUEST_TIMEOUT_SECONDS`: HTTP request timeout, default `15`
- `DOCKER_TIMEOUT_SECONDS`: Docker operation timeout, default `60`
- `AUTO_WARM_START`: create Tier 1 and Tier 2 on startup, default `true`
- `WARM_ON_INVOKE`: recreate warm tiers in the background after requests, default `true`

## How to Run It

Invoke the gateway:

```bash
curl -X POST http://localhost:8080/invoke \
  -H "Content-Type: application/json" \
  -d '{"request_id":"demo-1"}'
```

Optional fields:

- `duration_ms`: override the default simulated work duration.
- `force_tier`: explicitly test tier `0`, `1`, or `2` for controlled experiments.

Examples:

```bash
curl -X POST http://localhost:8080/invoke \
  -H "Content-Type: application/json" \
  -d '{"request_id":"cold-1","force_tier":0}'

curl -X POST http://localhost:8080/invoke \
  -H "Content-Type: application/json" \
  -d '{"request_id":"warm-1","force_tier":1}'

curl -X POST http://localhost:8080/invoke \
  -H "Content-Type: application/json" \
  -d '{"request_id":"hot-1","force_tier":2}'
```

## How to Test the Behavior

1. Start the server with `go run .`
2. Send several hot and warm requests quickly to show low-latency reuse.
3. Wait more than `30` seconds with no traffic.
4. Send another request and inspect the logs. The eviction worker should have removed idle Tier 1 and Tier 2 containers.
5. Open `metrics.csv` to confirm rows are being appended with:
   `Timestamp,RequestID,TierUsed,LatencyMS,ActiveContainersPoolSize`

You can also inspect Docker directly:

```bash
docker ps -a
```

During active periods you should see the resident Tier 1 and Tier 2 containers. After the idle timeout they should disappear.

## Load Testing

For repeatable experiments, use the built-in Go load generator:

```bash
go run ./cmd/loadtest -label cold-baseline -force-tier 0 -total 20 -rps 5 -concurrency 1
go run ./cmd/loadtest -label auto-burst -total 200 -rps 100 -concurrency 32
go run ./cmd/loadtest -label high-concurrency -seconds 3 -rps 1000 -concurrency 128 -duration-ms 50
```

Each run appends a row to `loadtest_summary.csv` with throughput, latency percentiles, and tier counts.

To execute the thesis workload end to end against a running server:

```bash
./scripts/run_thesis_workload.sh
```

## Generate Graphs

Install plotting dependencies:

```bash
python3 -m pip install pandas matplotlib
```

Then generate the charts:

```bash
python3 plot.py
```

This creates:

- `avg_latency_by_tier.png`
- `avg_latency_by_tier_all_requests.png`
- `pool_size_over_time.png`
- `latency_over_time_by_tier.png`
- `loadtest_latency_summary.png` when `loadtest_summary.csv` exists
