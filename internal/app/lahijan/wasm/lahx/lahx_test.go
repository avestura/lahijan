package lahx_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/lahx"
)

var lim = lahx.Limits{MaxManifestBytes: 1024, MaxModuleBytes: 4096}

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	in := lahx.Package{ManifestYAML: []byte("name: demo\n"), WasmBytes: []byte("\x00asm\x01\x00\x00\x00")}
	if err := lahx.Write(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := lahx.Open(buf.Bytes(), lim)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.ManifestYAML, in.ManifestYAML) || !bytes.Equal(out.WasmBytes, in.WasmBytes) {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestOpenRejects(t *testing.T) {
	t.Parallel()
	ok := map[string]string{lahx.ManifestEntry: "name: x", lahx.ModuleEntry: "wasm"}
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"not a zip", []byte("definitely not a zip"), lahx.ErrInvalid},
		{"missing manifest", zipOf(t, map[string]string{lahx.ModuleEntry: "w"}), lahx.ErrMissingManifest},
		{"missing module", zipOf(t, map[string]string{lahx.ManifestEntry: "m"}), lahx.ErrMissingModule},
		{"unknown entry", zipOf(t, map[string]string{lahx.ManifestEntry: "m", lahx.ModuleEntry: "w", "evil.sh": "x"}), lahx.ErrInvalid},
		{"traversal", zipOf(t, map[string]string{"../" + lahx.ModuleEntry: "w", lahx.ManifestEntry: "m"}), lahx.ErrInvalid},
		{"nested", zipOf(t, map[string]string{"sub/" + lahx.ModuleEntry: "w", lahx.ManifestEntry: "m"}), lahx.ErrInvalid},
		{"module too large", zipOf(t, map[string]string{lahx.ManifestEntry: "m", lahx.ModuleEntry: strings.Repeat("a", 5000)}), lahx.ErrTooLarge},
	}
	if _, err := lahx.Open(zipOf(t, ok), lim); err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := lahx.Open(tc.data, lim); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
