TINYGO     := tinygo
TARGET     := plugin.wasm
SRC        := main.go

.PHONY: build clean verify package

build: $(TARGET)

$(TARGET): $(SRC) go.mod lahijan.manifest.yaml
	$(TINYGO) build -target wasm -o $(TARGET) $(SRC)

verify: build
	@echo "Checking that $(TARGET) has no WASI imports..."
	@! strings $(TARGET) | grep -E '^(wasi_snapshot_preview1|wasi_unstable)$$' \
		|| (echo "FAIL: module imports WASI — Lahijan rejects WASI" && exit 1)
	@echo "OK: $(TARGET) declares no WASI imports"

# A .lahx is a ZIP with lahijan.manifest.yaml + plugin.wasm at its root.
# `lahx pack` validates the manifest first. Outside the Lahijan repo, point
# LAHIJAN_ROOT at a checkout, or use: zip -j my.lahx lahijan.manifest.yaml plugin.wasm
LAHIJAN_ROOT ?= ../../..
package: build
	go run -C $(LAHIJAN_ROOT) ./cmd/lahx pack $(CURDIR)

clean:
	rm -f $(TARGET) *.lahx
