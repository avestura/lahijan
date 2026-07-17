// Package database: helpers.go holds tiny unexported helpers that translate
// the "optional with default" API the repositories expose into the concrete
// types sqlc generates for NOT NULL columns backed by a column default.
package database

// boolOr returns *p when non-nil, otherwise def.
func boolOr(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}

// strOr returns *p when non-nil, otherwise def.
func strOr(p *string, def string) string {
	if p != nil {
		return *p
	}
	return def
}
