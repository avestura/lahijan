package main

import (
	"strings"
	"testing"
)

func TestParseCoverTotal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		profile string
		want    float64
		wantErr bool
	}{
		{
			name: "all covered",
			profile: strings.Join([]string{
				"mode: set",
				"github.com/foo/bar/x.go:1.1,2.2 5 1",
				"github.com/foo/bar/y.go:3.1,4.2 10 1",
			}, "\n"),
			want:    100.0,
			wantErr: false,
		},
		{
			name: "half covered",
			profile: strings.Join([]string{
				"mode: set",
				"github.com/foo/bar/x.go:1.1,2.2 10 1",
				"github.com/foo/bar/y.go:3.1,4.2 10 0",
			}, "\n"),
			want:    50.0,
			wantErr: false,
		},
		{
			name: "weighted by numstmts",
			profile: strings.Join([]string{
				"mode: set",
				"github.com/foo/bar/x.go:1.1,2.2 3 1",  // covered: 3
				"github.com/foo/bar/y.go:3.1,4.2 17 0", // not covered: 17
			}, "\n"),
			want:    15.0, // 3 of 20 = 15%
			wantErr: false,
		},
		{
			name: "empty profile (just mode)",
			profile: strings.Join([]string{
				"mode: set",
				"",
			}, "\n"),
			wantErr: true, // no statements
		},
		{
			name: "blank lines + mode header skipped",
			profile: strings.Join([]string{
				"",
				"mode: atomic",
				"",
				"github.com/foo/bar/x.go:1.1,2.2 4 2",
				"",
			}, "\n"),
			want:    100.0,
			wantErr: false,
		},
		{
			name: "malformed line rejected",
			profile: strings.Join([]string{
				"mode: set",
				"this is not a valid cover line",
			}, "\n"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCoverTotal(tc.profile)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %.2f", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %.2f, want %.2f", got, tc.want)
			}
		})
	}
}
