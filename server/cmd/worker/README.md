# Worker

Background task runner for the IoTMaker server. Consumes tasks from
Redis-backed Asynq queues and writes their results back to Redis (or
SQLite, depending on the task) for the HTTP layer to pick up.

## What it does

The worker handles four task types across four queues. Each queue has
a different priority weight so the scheduler picks tasks in proportion
to how interactive they are.

| Queue | Weight | Task type | Description |
|---|---|---|---|
| `codegen` | 5 | `codegen:run` | Runs the code generation pipeline (graph → IR → backend) against a scene exported by the IDE. Result published to `codegen:job:{id}:result` for SSE delivery. **Interactive — a maker is staring at a spinner.** |
| `devices` | 4 | `device:github` | Downloads a GitHub release ZIP, parses every IDS struct, extracts markdown help files and images, saves to SQLite. Interactive — a specialist clicked Submit and is waiting. |
| `templates` | 2 | `template:github` | Downloads a GitHub release ZIP, parses the full Go project, saves the parsed definition to SQLite. Background — the specialist usually moves on. |
| `cleanup` | 1 | (housekeeping) | Wizard draft garbage collection and similar hygiene tasks. |

The full task-type to handler map lives at the top of `main.go` —
read it there if this table drifts.

## Architecture context

The worker is one of three processes that make up the production
deployment:

```
┌──────────┐       ┌────────┐         ┌──────────┐
│  IDE     │ HTTP  │ Server │  Asynq  │  Worker  │
│  (WASM)  ├──────►│ (Echo) ├────────►│  (this)  │
└──────────┘       └───┬────┘         └────┬─────┘
                       │     SQLite/Redis  │
                       └───────────────────┘
```

The server enqueues tasks and never blocks on heavy work; the worker
runs the heavy work and never speaks HTTP. The contract between them
is Redis keys and Asynq task types, both stable over the lifetime of
a deployment.

## Running

In development:

```bash
make dev          # boots both server and worker in parallel
```

Standalone:

```bash
go run ./cmd/worker
```

The worker reads the same `config` package as the server: `REDIS_ADDR`,
`USER_FILES_DIR`, etc. See `server/config/`.

## Horizontal scaling

The worker is stateless. Multiple instances can run against the same
Redis instance and Asynq will distribute tasks among them. Useful when
a flood of codegen submissions arrives — the dedicated `codegen`
queue means heavy GitHub parse tasks on other queues do not starve
maker interactivity.

```bash
# docker compose: scale to three worker replicas
docker compose up --scale worker=3 -d
```

Each replica keeps its own connection pool to Redis. The pool size is
the Asynq default (10 goroutines per worker process) and is fine for
CPU-bound tasks like codegen; for IO-heavy tasks like GitHub parsing
the default is also fine because the bottleneck is the remote endpoint.

### Caveat — SQLite is not horizontal

A worker that touches SQLite (`device:github`, `template:github`,
`cleanup`) still reads from and writes to the single
`data/iotmaker.db` file. Scaling those queues to N replicas does not
multiply database throughput; the database becomes the contention
point. In the current single-host deployment this is acceptable
because SQLite handles modest concurrent writes well, but moving to
Postgres is on the roadmap and would lift this ceiling.

The codegen queue, by contrast, is fully horizontal: codegen does not
touch SQLite at all — it operates purely on the scene JSON and
serialised BlackBoxDefs that travel in the task payload. Scaling
`codegen` to 10 replicas multiplies codegen throughput by 10.

## Cancellation

The codegen handler observes `ctx` cancellation. The cancellation
sources are:

  1. The Asynq `Timeout(120s)` — bounds the worst-case runtime.
  2. `Inspector.CancelProcessing(taskID)` — triggered by the HTTP
     `POST /api/v1/codegen/jobs/{id}/cancel` endpoint, by an explicit
     user click on the IDE's progress overlay.
  3. `Inspector.CancelProcessing(taskID)` again — triggered by the SSE
     stream handler when the EventSource disconnects (browser close,
     F5, network drop).

The codegen pipeline checks `ctx.Err()` at four points between its
five stages (parse → build → validate → emit IR → backend), so a
cancellation observed mid-flight bounds the wasted CPU to "one
remaining pipeline stage", typically <500ms. Cancellation does NOT
happen inside graph.Build or ir.Emit themselves — those packages
do not yet accept context.

## Testing

```bash
go test ./cmd/worker/...
```

The current test (`codegen_test.go`) is white-box (package main),
backed by `miniredis` for an in-process Redis. No real worker server
is started; the handler is invoked directly with a synthetic
`*asynq.Task`. This pattern is the convention for any future worker
tests in this package.

## Adding a new task type

  1. Define the type and payload in `server/tasks/<name>.go`. Use
     `json:"camelCase"` tags on every payload field (project-wide
     invariant — without them, fields arrive `undefined` in JS).
  2. Pick a queue name and add it to the priority map in `main.go`.
     Reuse `codegen`, `devices`, `templates`, or `cleanup` if the
     interactivity profile matches; create a new queue if it doesn't.
  3. Write `makeXxxHandler(rdb)` in this directory, following the
     pattern in `makeCodegenHandler` / `makeDeviceGitHubHandler`.
  4. Register it with `mux.HandleFunc(tasks.TypeXxx, makeXxxHandler(rdb))`.
  5. Add a test in this directory that exercises the happy path and
     at least one failure path. Worker code that never runs in CI is
     code that drifts silently.
