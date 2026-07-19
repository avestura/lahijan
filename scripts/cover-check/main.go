// Package main implements scripts/cover-check: a tiny portable coverage-floor
// gate used by `make cover-check` (WS-22). The Go toolchain ships everything
// we need; rolling our own keeps the gate cross-platform (Linux CI + Windows
// local dev both run `go run ./scripts/cover-check`).
//
// Usage:
//
//	go run ./scripts/cover-check <coverage.out> [min_pct]
//
// Default floor: 20% statements (the WS-22 baseline; see ADR-0029 for why
// the gate is enforced at the current level and not the 60% MVP aspiration
// in the WS doc). Override with the second arg or the LAHIJAN_COVERAGE_MIN_PCT
// env var.
//
// The 20% default reflects WS-22 reality: the test architecture deliberately
// puts most coverage behind the `integration` build tag (testcontainers +
// fakes), so plain `go test` carries only the pure-logic tests. The gate
// prevents regressions against that baseline. Raising the floor is a
// follow-up task tracked in the WS-22 resolution notes.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/cover-check <coverage.out> [min_pct]")
		os.Exit(2)
	}
	path := os.Args[1]

	minPct := 20.0
	if v := os.Getenv("LAHIJAN_COVERAGE_MIN_PCT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			minPct = f
		}
	}
	if len(os.Args) >= 3 {
		if f, err := strconv.ParseFloat(os.Args[2], 64); err == nil {
			minPct = f
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::read %s: %v\n", path, err)
		fmt.Fprintln(os.Stderr, "Did you run 'make cover' first?")
		os.Exit(2)
	}

	total, err := parseCoverTotal(string(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::parse %s: %v\n", path, err)
		os.Exit(2)
	}

	fmt.Printf("coverage floor : %.1f%%\n", minPct)
	fmt.Printf("total coverage : %.1f%%\n", total)
	if total < minPct {
		fmt.Printf("::error::coverage %.1f%% is below the %.1f%% floor\n", total, minPct)
		fmt.Println("::error::add tests or adjust LAHIJAN_COVERAGE_MIN_PCT (reviewers must approve any drop)")
		os.Exit(1)
	}
	fmt.Printf("ok: coverage %.1f%% meets the %.1f%% floor\n", total, minPct)
}

// parseCoverTotal extracts the aggregate statement coverage percentage from
// a go-cover profile. The profile is line-oriented; each non-mode/header
// line carries `path:start_line.start_col,end_line.end_col numStmts count`
// and we total weighted by numStmts. Count > 0 means covered.
//
// We deliberately do NOT shell out to `go tool cover -func` because that
// re-parses per-function; the raw profile has the same data and is faster.
func parseCoverTotal(profile string) (float64, error) {
	var covered, total float64
	for i, line := range strings.Split(profile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// Format: path:start,end numStmts count
		// Split on the first space (path:range has no spaces; numStmts
		// and count are space-separated).
		spaceIdx := strings.IndexByte(line, ' ')
		if spaceIdx < 0 {
			return 0, fmt.Errorf("line %d: no space separator", i+1)
		}
		rest := line[spaceIdx+1:]
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			return 0, fmt.Errorf("line %d: expected <numStmts> <count>", i+1)
		}
		numStmts, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, fmt.Errorf("line %d: parse numStmts %q: %w", i+1, parts[0], err)
		}
		count, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, fmt.Errorf("line %d: parse count %q: %w", i+1, parts[1], err)
		}
		total += numStmts
		if count > 0 {
			covered += numStmts
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("no statements in profile")
	}
	return 100 * covered / total, nil
}
