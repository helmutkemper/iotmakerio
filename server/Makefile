# server/Makefile — IoTMaker Portal developer convenience targets.
#
# Usage:
#   make dev            Start server + worker in parallel (requires GNU make ≥ 4.3)
#   make server         Run the HTTP server (requires Redis running)
#   make worker         Run the background worker
#   make test           Run all tests
#   make lint           Run go vet on all packages
#   make tidy           Update go.sum and remove unused dependencies
#   make docker-up      Build WASM (if needed) then start all services
#   make docker-up-full Force WASM rebuild even if sources are unchanged
#   make docker-down    Stop and remove Docker containers
#   make clean-data     ⚠ Delete ALL local persistent data (DB + user files)
#   make clean-files    ⚠ Delete only user-uploaded project files (keep DB)

GO ?= go

# WASM output path inside the server public tree.
WASM_OUT := ./public/ide/main.wasm

# Root of the WASM source tree (parent of server/).
WASM_SRC_DIR := ..

# All .go files in the WASM source tree.
# Used by the conditional build target to detect changes.
WASM_SOURCES := $(shell find $(WASM_SRC_DIR) -name '*.go' \
    -not -path '$(WASM_SRC_DIR)/server/*' \
    -not -path '*/vendor/*' \
    2>/dev/null)

.PHONY: server worker test lint tidy docker-up docker-up-full docker-down \
        dev clean-data clean-files wasm wasm-force

# ── Local development ─────────────────────────────────────────────────────────

server:
	go run ./cmd/server

worker:
	go run ./cmd/worker

# Run server and worker side-by-side (requires GNU make ≥ 4.3 for -j)
dev:
	@echo "Starting server and worker…"
	$(MAKE) -j2 server worker

# ── Quality ───────────────────────────────────────────────────────────────────

test:
	go test ./... -v -race

lint:
	go vet ./...

tidy:
	go mod tidy

# ── WASM build — conditional ──────────────────────────────────────────────────

# wasm rebuilds main.wasm only when a .go source file is newer than the
# current main.wasm. Avoids the 30-60s WASM compile on every docker-up
# when only server-side Go or public/ files changed.
$(WASM_OUT): $(WASM_SOURCES)
	@echo "→ Building WASM (source changed)…"
	cd $(WASM_SRC_DIR) && $(GO) mod tidy
	cd $(WASM_SRC_DIR) && GOARCH=wasm GOOS=js $(GO) build -o main.wasm
	cp $(WASM_SRC_DIR)/main.wasm $(WASM_OUT)
	@echo "✓ WASM written to $(WASM_OUT)"

wasm: $(WASM_OUT)

# wasm-force ignores timestamps and always recompiles.
wasm-force:
	@echo "→ Force-building WASM…"
	cd $(WASM_SRC_DIR) && $(GO) mod tidy
	cd $(WASM_SRC_DIR) && GOARCH=wasm GOOS=js $(GO) build -o main.wasm
	cp $(WASM_SRC_DIR)/main.wasm $(WASM_OUT)
	@echo "✓ WASM written to $(WASM_OUT)"

# ── Docker ────────────────────────────────────────────────────────────────────

# docker-up rebuilds WASM only if needed, then starts the services.
# Typical cycle after editing only server Go code or public/ files:
#   WASM build is skipped → docker-compose up --build in ~10s.
docker-up: wasm
	docker-compose build --no-cache
	docker-compose up --force-recreate

# docker-up-full always recompiles WASM before starting.
# Use this after pulling new WASM commits or when the timestamp trick fails.
docker-up-full: wasm-force
	docker-compose up --build -d

# docker-up-clean force-rebuilds BOTH images with --no-cache before starting.
# Reach for it when behaviour contradicts the source: `up --build` can serve
# CACHED layers for one service while rebuilding the other — field lesson
# 2026-07-12, a stale server container kept shipping hybrid black-box defs
# for three exports while the worker (and the source tree) were current.
# Português: Rebuild forçado das DUAS imagens sem cache. Use quando o
# comportamento contradiz a fonte: `up --build` pode servir camadas de
# CACHE para um serviço e rebuildar o outro — lição de campo 2026-07-12.
docker-up-clean: wasm-force
	docker-compose build --no-cache
	docker-compose up -d

docker-down:
	docker-compose down

# ── Data cleanup ──────────────────────────────────────────────────────────────

clean-data:
	@echo ""
	@echo "  ⚠  WARNING: this will permanently delete:"
	@echo "       ./data/iotmaker.db   — all users, projects and code versions"
	@echo "       ./data/files/        — all uploaded code, images and docs"
	@echo ""
	@printf "  Type YES to confirm: "; read ans; \
	  if [ "$$ans" = "YES" ]; then \
	    $(MAKE) docker-down 2>/dev/null || true; \
	    mkdir -p .trash; \
	    mv ./data .trash/data-$$(date +%Y%m%d-%H%M%S); \
	    echo "  ✓ ./data moved to .trash/ (recoverable until you empty it)."; \
	    echo "    Purge old snapshots with: make clean-trash"; \
	  else \
	    echo "  Aborted — nothing was deleted."; \
	  fi

## clean-trash — permanently remove every data snapshot parked by
## clean-data. This is the ONLY place rm -rf touches user data.
## Português: Remove de vez os snapshots estacionados pelo clean-data —
## o ÚNICO lugar onde rm -rf toca dado de usuário.
clean-trash:
	rm -rf .trash
	@echo "  ✓ .trash emptied."

clean-files:
	@echo ""
	@echo "  ⚠  WARNING: this will permanently delete:"
	@echo "       ./data/files/   — all uploaded code, images and docs"
	@echo "  The database (users, projects, versions) will NOT be touched."
	@echo ""
	@printf "  Type YES to confirm: "; read ans; \
	  if [ "$$ans" = "YES" ]; then \
	    rm -rf ./data/files; \
	    echo "  ✓ ./data/files removed."; \
	  else \
	    echo "  Aborted — nothing was deleted."; \
	  fi
