// Package wasiimporter builds the wazero WASI Preview 1 configuration for a
// WASI-mode plugin based on its granted permissions (WS-10e / ADR-0039).
//
// The importer translates WASI permission slugs (wasi.fs.preopen,
// wasi.env, wasi.clock, ...) into wazero's ModuleConfig knobs:
// FSConfig (per-directory preopens) and env vars (WithEnv). The runtime
// uses the returned ModuleConfig when instantiating a WASI plugin so each
// plugin sees only the filesystem + env it was granted.
//
// Security model: WASI Preview 1 is registered in full via
// wasi_snapshot_preview1.InstantiateSnapshotPreview1 (all functions are
// present), but the filesystem is restricted to granted preopens and the
// environment is restricted to granted vars. A plugin that imports
// path_open but has no wasi.fs.preopen grant will get EBADF / ENOENT at
// runtime because no directories are preopened. This is configuration-level
// filtering, not import-level filtering — see ADR-0039 "Notes" for the
// rationale.
package wasiimporter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

// EnsureWASI registers the full wasi_snapshot_preview1 host module on the
// runtime. Called once during runtime.New when WASI mode is enabled in
// config. Not idempotent — wazero rejects a duplicate registration.
func EnsureWASI(ctx context.Context, rt wazero.Runtime) error {
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return fmt.Errorf("wasiimporter: register WASI P1: %w", err)
	}
	return nil
}

// ModuleConfigBuilder collects the per-plugin WASI configuration derived
// from the plugin's granted slugs + the manifest's wasi: block. The
// runtime uses it during InstantiateModule to pass a per-plugin
// ModuleConfig with the right FSConfig + env vars.
type ModuleConfigBuilder struct {
	log *slog.Logger
}

// NewBuilder constructs a ModuleConfigBuilder. The logger receives
// warnings about malformed grants or missing directories.
func NewBuilder(log *slog.Logger) *ModuleConfigBuilder {
	if log == nil {
		log = slog.Default()
	}
	return &ModuleConfigBuilder{log: log}
}

// Build creates a wazero.ModuleConfig for a WASI plugin based on its
// grants + manifest. The fsRoot is the operator-configured sandbox root
// (conf.wasm.wasi.fs_root); each plugin's files live under
// <fsRoot>/<pluginSlug>/.
//
// The grants slice is the full list of permission slugs the plugin holds
// (from the enforcer / plugin_permissions table). The wasiPreopens and
// wasiEnv come from the manifest's wasi: block (they map the grant
// qualifiers to concrete mount paths + env var names).
func (b *ModuleConfigBuilder) Build(
	pluginSlug string,
	grants []string,
	wasiPreopens []PreopenSpec,
	wasiEnv []string,
	envValues map[string]string,
	fsRoot string,
) (wazero.ModuleConfig, error) {
	if pluginSlug == "" {
		return nil, errors.New("wasiimporter: pluginSlug is required")
	}
	if fsRoot == "" {
		return nil, errors.New("wasiimporter: fsRoot is required")
	}

	cfg := wazero.NewModuleConfig()

	// God-mode: * grant → full WASI surface, sandbox to <fsRoot>/<pluginSlug>/.
	if hasWildcard(grants) {
		pluginDir := filepath.Join(fsRoot, pluginSlug)
		if err := ensureDir(pluginDir); err != nil {
			return nil, fmt.Errorf("wasiimporter: ensure plugin dir for wildcard: %w", err)
		}
		fsCfg := wazero.NewFSConfig().WithDirMount(pluginDir, "/")
		cfg = cfg.WithFSConfig(fsCfg)
		b.log.Warn("wasiimporter: plugin granted * (full WASI surface)",
			"plugin", pluginSlug)
		return cfg, nil
	}

	// Filtered: mount only granted preopens + inject only granted env vars.
	fsBuilder := wazero.NewFSConfig()
	hasPreopens := false

	for _, spec := range wasiPreopens {
		mode := spec.Mode
		if mode == "" {
			mode = "ro"
		}

		// Check if the plugin holds this exact slug or a wildcard variant.
		if !grantedForPreopen(grants, spec.GuestPath, mode) {
			b.log.Debug("wasiimporter: preopen not granted, skipping",
				"plugin", pluginSlug, "guestPath", spec.GuestPath, "mode", mode)
			continue
		}

		hostPath := filepath.Join(fsRoot, pluginSlug, spec.HostSubdir)
		if err := ensureDir(hostPath); err != nil {
			b.log.Warn("wasiimporter: cannot create preopen dir, skipping",
				"plugin", pluginSlug, "hostPath", hostPath, "error", err)
			continue
		}
		if mode == "rw" {
			fsBuilder = fsBuilder.WithDirMount(hostPath, spec.GuestPath)
		} else {
			fsBuilder = fsBuilder.WithReadOnlyDirMount(hostPath, spec.GuestPath)
		}
		hasPreopens = true
	}

	if hasPreopens {
		cfg = cfg.WithFSConfig(fsBuilder)
	}

	// Inject granted env vars.
	for _, envName := range wasiEnv {
		slug := permission.CapWasiEnv + ":" + envName
		if !permissionAllowed(grants, slug) {
			b.log.Debug("wasiimporter: env not granted, skipping",
				"plugin", pluginSlug, "env", envName)
			continue
		}
		val, ok := envValues[envName]
		if !ok {
			b.log.Warn("wasiimporter: env granted but no value injected by admin",
				"plugin", pluginSlug, "env", envName)
			continue
		}
		cfg = cfg.WithEnv(envName, val)
	}

	return cfg, nil
}

// PreopenSpec mirrors manifest.WasiPreopen in a flat, import-cycle-free
// shape so this package does not depend on the manifest package.
type PreopenSpec struct {
	GuestPath  string
	HostSubdir string
	Mode       string
}

// hasWildcard reports whether the grant list contains the * god-mode slug.
func hasWildcard(grants []string) bool {
	for _, g := range grants {
		if g == permission.CapWasiWildcard {
			return true
		}
	}
	return false
}

// grantedForPreopen reports whether the grants allow mounting at guestPath
// with the given mode. Matches:
//   - exact: wasi.fs.preopen:/data:rw
//   - any-mode: wasi.fs.preopen:/data:* or wasi.fs.preopen:/data:*
//   - any-path: wasi.fs.preopen:* (mount anything)
func grantedForPreopen(grants []string, guestPath, mode string) bool {
	exact := permission.CapWasifSPreopen + ":" + guestPath + ":" + mode
	if permissionAllowed(grants, exact) {
		return true
	}
	pathWildcard := permission.CapWasifSPreopen + ":" + guestPath + ":*"
	if permissionAllowed(grants, pathWildcard) {
		return true
	}
	allWildcard := permission.CapWasifSPreopen + ":*"
	if permissionAllowed(grants, allWildcard) {
		return true
	}
	return false
}

// permissionAllowed is a thin wrapper around permission.Allowed for
// convenience. We don't import the enforcer here; the runtime passes the
// full grant list so we can check locally without a DB round-trip.
func permissionAllowed(grants []string, requested string) bool {
	return permission.Allowed(grants, requested)
}

// ensureDir creates a directory if it does not exist, including parents.
func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return nil
}

// ParseWasiMode extracts the mode from a preopen slug qualifier.
// Given "/data:rw" returns ("rw", true). Given "/data" returns ("ro", true).
func ParseWasiMode(qualifier string) (mode string, ok bool) {
	idx := strings.LastIndex(qualifier, ":")
	if idx < 0 {
		return "ro", true // default
	}
	mode = qualifier[idx+1:]
	switch mode {
	case "ro", "rw":
		return mode, true
	default:
		return "", false
	}
}
