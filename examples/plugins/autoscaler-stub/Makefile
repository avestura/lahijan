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
