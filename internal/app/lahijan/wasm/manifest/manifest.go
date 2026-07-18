// Package manifest defines the schema for lahijan.manifest.yaml, the file a
// plugin ships alongside its .wasm module to declare what it needs.
//
// The manifest is the single source of truth at install time: every
// permission the plugin requests MUST be in the manifest, and every
// permission in the manifest MUST be in the catalog
// (internal/app/lahijan/wasm/permission). The admin sees exactly this list
// when approving grants (per ADR-0012).
//
// Schema (YAML):
//
//	name: slack-notifier               # required, human-friendly, kebab-case
//	version: 1.2.0                     # required, semver
//	description: "Posts alerts to Slack"
//	author: "Example Corp <ops@example.com>"
//	license: Apache-2.0                # SPDX identifier
//	homepage: https://example.com/slack-notifier
//
//	permissions:                       # required, may be empty
//	  - network.outbound
//	  - kv.read:cache
//	  - events.listen:dns.record.*
//
//	config_schema:                     # optional, JSON-schema-ish shape
//	  type: object
//	  properties:
//	    webhook_url:
//	      type: string
//	      format: uri
//	      secret: true                 # value will be encrypted at rest
//	    channel:
//	      type: string
//	      default: "#alerts"
//
//	entrypoints:                       # required: which exports the runtime may call
//	  - on_event                       # called when a subscribed event fires
//	  - on_request                     # called for each registered HTTP route
//
// memory_pages and max_memory_pages are NOT in the manifest. The runtime
// derives them from the .wasm module's declared memory max (which the
// compiler emits) and clamps to the process-wide conf.wasm.max_memory_per_plugin.
// This keeps the manifest focused on what the plugin NEEDS, not on
// operational limits the operator controls.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

// Manifest mirrors lahijan.manifest.yaml. Fields are public so encoding/yaml
// can unmarshal directly. JSON tags are present so the same struct round-trips
// through the database (plugins.manifest_json is JSONB).
type Manifest struct {
	Name        string         `yaml:"name"       json:"name"`
	Version     string         `yaml:"version"    json:"version"`
	Description string         `yaml:"description" json:"description,omitempty"`
	Author      string         `yaml:"author"     json:"author,omitempty"`
	License     string         `yaml:"license"    json:"license,omitempty"`
	Homepage    string         `yaml:"homepage"   json:"homepage,omitempty"`

	Permissions []string         `yaml:"permissions"  json:"permissions,omitempty"`
	ConfigSchema *map[string]any `yaml:"config_schema" json:"config_schema,omitempty"`

	Entrypoints []string `yaml:"entrypoints" json:"entrypoints,omitempty"`
}

// Validate enforces the manifest's structural invariants:
//
//   - Name + Version are required.
//   - Name is kebab-case (lowercase letters, digits, single dashes).
//   - Version parses as semver (MAJOR.MINOR.PATCH).
//   - Every permission validates via permission.Validate.
//   - Entrypoints (when present) are non-empty identifiers.
//
// Validate does NOT check the .wasm module itself; the runtime does that at
// compile time (memory shape, imports, exports).
func (m *Manifest) Validate() error {
	var errs []error
	if err := validateName(m.Name); err != nil {
		errs = append(errs, fmt.Errorf("manifest: name: %w", err))
	}
	if err := validateVersion(m.Version); err != nil {
		errs = append(errs, fmt.Errorf("manifest: version: %w", err))
	}
	for i, p := range m.Permissions {
		if err := permission.Validate(p); err != nil {
			errs = append(errs, fmt.Errorf("manifest: permissions[%d] %q: %w", i, p, err))
		}
	}
	for i, e := range m.Entrypoints {
		if strings.TrimSpace(e) == "" {
			errs = append(errs, fmt.Errorf("manifest: entrypoints[%d] is empty", i))
		}
	}
	if len(errs) == 1 {
		return errs[0]
	}
	if len(errs) > 1 {
		// Join the messages so the admin sees every problem in one shot
		// rather than fixing one at a time.
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return errors.New("manifest: multiple errors:\n  - " + strings.Join(msgs, "\n  - "))
	}
	return nil
}

// Parse decodes YAML bytes into a Manifest and runs Validate. Callers
// should pass the raw bytes from the upload payload; this is the only
// entry point that turns bytes into a validated Manifest.
func Parse(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse yaml: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseJSON decodes JSON bytes into a Manifest (used when reading back from
// the DB's manifest_json column). Does NOT re-run Validate; the manifest
// was validated at insert time and the round-trip is identity-preserving.
func ParseJSON(raw []byte) (*Manifest, error) {
	if len(raw) == 0 {
		return &Manifest{}, nil
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse json: %w", err)
	}
	return &m, nil
}

// MarshalJSON returns the manifest as compact JSON for storage. Used by the
// installer before persisting to plugins.manifest_json.
func (m *Manifest) MarshalJSON() ([]byte, error) {
	type alias Manifest // prevent recursion through yaml tags
	out, err := json.Marshal((*alias)(m))
	if err != nil {
		return nil, fmt.Errorf("manifest: marshal json: %w", err)
	}
	return out, nil
}

// validateName enforces kebab-case + non-empty. The pattern is tight on
// purpose: the name shows up in URL paths (/api/v1/plugins/<name>/...) so
// it must be URL-safe.
func validateName(name string) error {
	if name == "" {
		return errors.New("is required")
	}
	if len(name) > 64 {
		return errors.New("must be <= 64 characters")
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !ok {
			return fmt.Errorf("contains invalid character %q at index %d (allowed: a-z 0-9 -)", r, i)
		}
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return errors.New("must not start or end with a dash")
	}
	if strings.Contains(name, "--") {
		return errors.New("must not contain consecutive dashes")
	}
	return nil
}

// validateVersion enforces MAJOR.MINOR.PATCH with optional pre-release /
// build suffixes (subset of semver). The full semver spec allows some odd
// shapes; we accept the common ones.
func validateVersion(v string) error {
	if v == "" {
		return errors.New("is required")
	}
	parts := strings.SplitN(v, ".", 4)
	if len(parts) < 3 {
		return fmt.Errorf("must be MAJOR.MINOR.PATCH, got %q", v)
	}
	for _, p := range parts[:3] {
		// Strip an optional pre-release/build suffix on PATCH.
		clean := strings.SplitN(p, "+", 2)[0]
		clean = strings.SplitN(clean, "-", 2)[0]
		if clean == "" {
			return fmt.Errorf("version segment in %q is empty", v)
		}
		for _, r := range clean {
			if r < '0' || r > '9' {
				return fmt.Errorf("version segment %q in %q must be numeric", clean, v)
			}
		}
	}
	return nil
}
