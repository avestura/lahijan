# Lahijan plugin Makefile.
#
# Builds the plugin to a plain wasm32-unknown-unknown module via TinyGo
# (per ADR-0023). The resulting .wasm has the imports declared in main.go
# (e.g. lahijan_kv.set) and zero ambient surface — no WASI, no filesystem,
# no env. Every capability the plugin needs is a Lahijan host function,
# gated by the permission enforcer.
#
# Prerequisites:
#   - tinygo >= 0.32 on PATH (https://tinygo.org/getting-started/install/)
#
# Targets:
#   make build   - compile plugin.wasm
#   make clean   - remove plugin.wasm
#   make verify  - build + assert the module declares no WASI imports

TINYGO     := tinygo
TARGET     := plugin.wasm
SRC        := main.go

.PHONY: build clean verify

build: $(TARGET)

$(TARGET): $(SRC) go.mod lahijan.manifest.yaml
	$(TINYGO) build -target wasm -o $(TARGET) $(SRC)

verify: build
	@echo "Checking that $(TARGET) has no WASI imports..."
	@! strings $(TARGET) | grep -E '^(wasi_snapshot_preview1|wasi_unstable)$$' \
		|| (echo "FAIL: module imports WASI — Lahijan rejects WASI" && exit 1)
	@echo "OK: $(TARGET) declares no WASI imports"

clean:
	rm -f $(TARGET)
