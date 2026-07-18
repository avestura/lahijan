// Package marketplace is the WS-10c scaffolding for browsing and
// installing plugins from a curated index. The index format lives at
// examples/plugins/marketplace/plugins-marketplace.yaml (the in-repo
// default); operators can replace the file or point
// conf.wasm.marketplace.url at a remote URL serving the same shape.
//
// Layering:
//
//	api (admin_marketplace_handlers.go)
//	  → marketplace.Service      ← this file
//	    → IndexLoader            (reads plugins-marketplace.yaml)
//	    → installer.Service      (the existing install path)
//	    → wasm.manifest.Parser   (parses + validates the manifest)
//
// The marketplace never writes to the plugins table directly. Install +
// upgrade go through the installer.Service so the audit emission,
// permission enforcement, and lifecycle hooks all stay in one place.
//
// The marketplace index is intentionally a separate file from the
// manifest: the index lists what's available + a sha256 pin; the
// manifest is the plugin's own declaration of what it needs. The
// installer verifies the downloaded wasm bytes match the index's sha256
// before persisting, so a tampered marketplace cannot silently swap a
// plugin binary.
package marketplace

// Index is the top-level shape of plugins-marketplace.yaml.
//
// Every field is plain-vanilla YAML so the file is hand-editable. The
// schema is documented in examples/plugins/marketplace/README.md.
type Index struct {
	Version   int       `yaml:"version"`
	UpdatedAt string    `yaml:"updated_at"`
	Plugins   []Entry   `yaml:"plugins"`
}

// Entry is one plugin's row in the marketplace index.
type Entry struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Author      string   `yaml:"author"`
	License     string   `yaml:"license"`
	Homepage    string   `yaml:"homepage"`
	Permissions []string `yaml:"permissions"`
	Source      Source   `yaml:"source"`
	SHA256      string   `yaml:"sha256"`
}

// Source tells the loader how to fetch the .wasm + manifest.
//
//   - repo: local  — the bytes live alongside the index, at
//     <marketplace_dir>/<path>/plugin.wasm and .../lahijan.manifest.yaml.
//     The default in-repo marketplace uses this shape.
//   - repo: git    — clone the git URL at the pinned ref. Signature
//     verification (cosign/sigstore) is REQUIRED for git sources in
//     marketplace installs (per WS-10c "Open questions" item 2); the
//     installer rejects unsigned git installs.
type Source struct {
	Repo string `yaml:"repo"`
	// Path is the subdirectory under the marketplace root for repo=local.
	Path string `yaml:"path"`
	// GitURL + GitRef pin the source for repo=git.
	GitURL string `yaml:"git_url"`
	GitRef string `yaml:"git_ref"`
}
