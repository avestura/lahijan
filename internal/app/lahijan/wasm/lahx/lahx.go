// Package lahx reads and writes Lahijan extension packages (`.lahx`).
//
// A `.lahx` file is a ZIP archive holding exactly two entries at its root:
//
//	lahijan.manifest.yaml   the plugin manifest (see wasm/manifest)
//	plugin.wasm             the compiled WebAssembly module
//
// ZIP was chosen because every OS can create and inspect it with stock
// tools (`zip -j my.lahx lahijan.manifest.yaml plugin.wasm`), it supports
// random access, and it is the container behind comparable formats (.jar,
// .vsix, .xpi). Entries may be stored or deflated.
//
// Open treats the archive as untrusted input: it rejects path traversal,
// nested paths, duplicate or unknown entries, encrypted entries, and caps
// both the declared and the actually-decompressed size of every entry so a
// zip bomb cannot exhaust memory.
package lahx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Extension is the file extension of a Lahijan extension package.
const Extension = ".lahx"

// MediaType is the IANA-style media type used for uploads and downloads.
const MediaType = "application/vnd.lahijan.extension+zip"

// Entry names inside a package.
const (
	ManifestEntry = "lahijan.manifest.yaml"
	ModuleEntry   = "plugin.wasm"
)

// maxEntries bounds the central directory so a crafted archive with
// millions of (empty) entries is rejected before it is walked.
const maxEntries = 16

// Limits caps the decompressed size of each entry.
type Limits struct {
	// MaxManifestBytes caps lahijan.manifest.yaml.
	MaxManifestBytes int64
	// MaxModuleBytes caps plugin.wasm.
	MaxModuleBytes int64
}

// Package is the decoded content of a `.lahx` file.
type Package struct {
	ManifestYAML []byte
	WasmBytes    []byte
}

// Sentinel errors so callers can map them to localized messages.
var (
	// ErrInvalid means the bytes are not a well-formed `.lahx` archive.
	ErrInvalid = errors.New("lahx: invalid extension package")
	// ErrMissingManifest means lahijan.manifest.yaml is absent.
	ErrMissingManifest = errors.New("lahx: package has no " + ManifestEntry)
	// ErrMissingModule means plugin.wasm is absent.
	ErrMissingModule = errors.New("lahx: package has no " + ModuleEntry)
	// ErrTooLarge means an entry exceeds its Limits cap.
	ErrTooLarge = errors.New("lahx: package entry too large")
)

// Open decodes and validates a `.lahx` archive held in memory.
func Open(data []byte, lim Limits) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(zr.File) > maxEntries {
		return nil, fmt.Errorf("%w: %d entries (max %d)", ErrInvalid, len(zr.File), maxEntries)
	}
	pkg := &Package{}
	for _, f := range zr.File {
		name := f.Name
		// Tolerate directory entries some tools emit; they carry no data.
		if strings.HasSuffix(name, "/") {
			continue
		}
		if err := checkName(name); err != nil {
			return nil, err
		}
		if f.Flags&0x1 != 0 {
			return nil, fmt.Errorf("%w: entry %q is encrypted", ErrInvalid, name)
		}
		var (
			dst   *[]byte
			limit int64
		)
		switch name {
		case ManifestEntry:
			dst, limit = &pkg.ManifestYAML, lim.MaxManifestBytes
		case ModuleEntry:
			dst, limit = &pkg.WasmBytes, lim.MaxModuleBytes
		default:
			return nil, fmt.Errorf("%w: unexpected entry %q (only %s and %s are allowed)",
				ErrInvalid, name, ManifestEntry, ModuleEntry)
		}
		if *dst != nil {
			return nil, fmt.Errorf("%w: duplicate entry %q", ErrInvalid, name)
		}
		body, err := readEntry(f, limit)
		if err != nil {
			return nil, err
		}
		*dst = body
	}
	if pkg.ManifestYAML == nil {
		return nil, ErrMissingManifest
	}
	if pkg.WasmBytes == nil {
		return nil, ErrMissingModule
	}
	return pkg, nil
}

// Write encodes a package as a `.lahx` archive (manifest first, then the
// deflated module). Used by the packaging CLI, the marketplace tooling and
// tests.
func Write(w io.Writer, pkg Package) error {
	zw := zip.NewWriter(w)
	for _, e := range []struct {
		name string
		body []byte
	}{{ManifestEntry, pkg.ManifestYAML}, {ModuleEntry, pkg.WasmBytes}} {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate})
		if err != nil {
			return fmt.Errorf("lahx: write %s: %w", e.name, err)
		}
		if _, err := fw.Write(e.body); err != nil {
			return fmt.Errorf("lahx: write %s: %w", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("lahx: finalize archive: %w", err)
	}
	return nil
}

// checkName rejects anything but a plain root-level file name.
func checkName(name string) error {
	if name == "" || strings.ContainsAny(name, `\:`) || strings.Contains(name, "/") ||
		path.Clean(name) != name || name == "." || name == ".." {
		return fmt.Errorf("%w: entry %q must be a plain file at the archive root", ErrInvalid, name)
	}
	return nil
}

// readEntry decompresses one entry, enforcing limit on both the declared
// size and the bytes actually produced (the header can lie).
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if limit > 0 && f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%w: %s is %d bytes (max %d)", ErrTooLarge, f.Name, f.UncompressedSize64, limit)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %w", ErrInvalid, f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	var r io.Reader = rc
	if limit > 0 {
		r = io.LimitReader(rc, limit+1)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrInvalid, f.Name, err)
	}
	if limit > 0 && int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, f.Name, limit)
	}
	return body, nil
}
