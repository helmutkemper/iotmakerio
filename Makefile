GO ?= go

.PHONY: help
## Este comando de ajuda
help:
	@printf "Opções de comandos\n\n"
	@awk '/^[a-zA-Z\-\_0-9]+:/ { \
		helpMessage = match(lastLine, /^## (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 3, RLENGTH); \
			printf "make %-30s ## %s\n", helpCommand, helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)

.PHONY: llama
## call llama cli
llama:
	@llama-cli \
       -m ~/models/Qwen2.5-Coder-7B-Instruct-Q4_K_M.gguf \
       -ngl 99 \
       -c 16384 \
       --temp 0.3 \
       -cnv

.PHONY: buildandrun
## build this example and run local server
buildandrun:
	@$(GO) mod tidy
	@GOARCH=wasm GOOS=js $(GO) build -o main.wasm
	@$(MAKE) -C ../../server build

.PHONY: build
## build this example
build:
	$(GO) mod tidy
	GOARCH=wasm GOOS=js $(GO) build -o main.wasm
	cp main.wasm ./serverIA/public/ide/main.wasm


.PHONY: server
## run local server
server:
	@$(GO) mod tidy
	@$(MAKE) -C ../../server build


# ── WASM test runner ──────────────────────────────────────────────────────────
#
# wasmbrowsertest is a Go binary that runs WASM tests inside a headless
# Chrome. It must live at $GOPATH/bin/go_js_wasm_exec because the `go test`
# toolchain looks for that exact filename when GOARCH=wasm.
#
# One-time setup per developer machine:
#   make install-wasm-runner
#
# Verify it is installed:
#   make check-wasm-runner
#
# Run the tests:
#   make test-wasm

# Path where Go expects to find the WASM runner.
WASM_RUNNER := $(shell go env GOPATH)/bin/go_js_wasm_exec

.PHONY: install-wasm-runner
install-wasm-runner:
	@echo "→ Installing wasmbrowsertest from github.com/agnivade/wasmbrowsertest…"
	$(GO) install github.com/agnivade/wasmbrowsertest@latest
	@echo "→ Renaming binary to go_js_wasm_exec (convention expected by go test)…"
	mv "$$(go env GOPATH)/bin/wasmbrowsertest" "$(WASM_RUNNER)"
	@echo "✓ Installed at $(WASM_RUNNER)"
	@echo ""
	@echo "  Make sure $$(go env GOPATH)/bin is in your \$$PATH. Add to ~/.zshrc"
	@echo "  (or ~/.bashrc on Linux):"
	@echo ""
	@echo "      export PATH=\"\$$PATH:\$$(go env GOPATH)/bin\""
	@echo ""
	@echo "  Then reload with 'source ~/.zshrc' (or open a new terminal)."

.PHONY: check-wasm-runner
check-wasm-runner:
	@if [ ! -x "$(WASM_RUNNER)" ]; then \
	  echo "✗ WASM runner not found at $(WASM_RUNNER)"; \
	  echo "  Run 'make install-wasm-runner' first."; \
	  exit 1; \
	fi
	@echo "✓ WASM runner present at $(WASM_RUNNER)"

.PHONY: test-wasm
test-wasm: check-wasm-runner
	GOARCH=wasm GOOS=js $(GO) test ./ui/mainMenu/... \
	    -exec="$(WASM_RUNNER)" -v

# Same tests but opens a visible Chrome window. Useful for DevTools
# inspection while debugging a failing test.
.PHONY: test-wasm-visual
test-wasm-visual: check-wasm-runner
	WASM_HEADLESS=off GOARCH=wasm GOOS=js $(GO) test ./ui/mainMenu/... \
	    -exec="$(WASM_RUNNER)" -v

## make apply T=pacote.tar.gz — aplica um tarball de entrega com validação
## (raiz, whitelist, anti-ninho, resumo, confirmação). Sem T=, pega o mais
## novo de ~/Downloads. Português: aplicador seguro dos pacotes.
apply:
	@./tools/apply-patch.sh $(T)
