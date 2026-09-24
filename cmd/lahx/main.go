// Command lahx builds and inspects Lahijan extension packages (`.lahx`).
//
//	go run ./cmd/lahx pack    <plugin-dir> [-o out.lahx]
//	go run ./cmd/lahx inspect <file.lahx>
//
// `pack` reads <plugin-dir>/lahijan.manifest.yaml and <plugin-dir>/plugin.wasm,
// validates the manifest exactly as the server will, and writes
// <name>-<version>.lahx (or -o). `inspect` validates an existing package and
// prints its manifest summary. A `.lahx` is a plain ZIP, so
// `zip -j my.lahx lahijan.manifest.yaml plugin.wasm` works too; this tool just
// catches manifest mistakes before upload.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/lahx"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
)

// Generous local caps: the server enforces its configured limits on upload.
var limits = lahx.Limits{MaxManifestBytes: 64 << 10, MaxModuleBytes: 64 << 20}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lahx:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: lahx pack <plugin-dir> [-o out.lahx] | lahx inspect <file.lahx>")
	}
	switch args[0] {
	case "pack":
		return pack(args[1:])
	case "inspect":
		return inspect(args[1:])
	default:
		return fmt.Errorf("unknown command %q (want pack or inspect)", args[0])
	}
}

func pack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default <name>-<version>.lahx in the plugin dir)")
	dir := "."
	// Accept the directory before or after the flags.
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		dir, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	manifestYAML, err := os.ReadFile(filepath.Join(dir, lahx.ManifestEntry))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	m, err := manifest.Parse(manifestYAML)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", lahx.ManifestEntry, err)
	}
	wasm, err := os.ReadFile(filepath.Join(dir, lahx.ModuleEntry))
	if err != nil {
		return fmt.Errorf("read module (build it first, e.g. `make build`): %w", err)
	}
	if !bytes.HasPrefix(wasm, []byte("\x00asm")) {
		return fmt.Errorf("%s is not a WebAssembly module", lahx.ModuleEntry)
	}
	target := *out
	if target == "" {
		target = filepath.Join(dir, fmt.Sprintf("%s-%s%s", m.Name, m.Version, lahx.Extension))
	}
	var buf bytes.Buffer
	if err := lahx.Write(&buf, lahx.Package{ManifestYAML: manifestYAML, WasmBytes: wasm}); err != nil {
		return err
	}
	if err := os.WriteFile(target, buf.Bytes(), 0o644); err != nil { //nolint:gosec // packages are meant to be shared
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Printf("wrote %s (%s %s, %d bytes)\n", target, m.Name, m.Version, buf.Len())
	return nil
}

func inspect(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: lahx inspect <file.lahx>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	pkg, err := lahx.Open(data, limits)
	if err != nil {
		return err
	}
	m, err := manifest.Parse(pkg.ManifestYAML)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", lahx.ManifestEntry, err)
	}
	fmt.Printf("%s %s\n  module: %d bytes\n  permissions: %v\n", m.Name, m.Version, len(pkg.WasmBytes), m.Permissions)
	return nil
}
