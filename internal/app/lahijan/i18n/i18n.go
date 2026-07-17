// Package i18n provides Lahijan's backend message bundles and the T() helper
// that every user-facing string goes through (ADR-0017).
//
// Locale JSON files live in locales/ and are embedded into the binary. The
// active locale is carried on the request context (set by middleware from the
// Accept-Language header or the user's profile locale) and read back by T.
//
// Default locale: en. Supported locales today: en, fa.
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFS embed.FS

const (
	// DefaultLocale is the platform default locale code (ADR-0017).
	DefaultLocale = "en"
)

var (
	// supportedTags is the set of locales Lahijan ships bundles for, in the
	// order they should be matched against the Accept-Language header.
	supportedTags = []language.Tag{
		language.English,
		language.Persian,
	}

	// matcher resolves an Accept-Language header (or a single code) to one of
	// the supported tags.
	matcher = language.NewMatcher(supportedTags)

	bundleOnce sync.Once
	bundle     *i18n.Bundle
	errBundle  error
)

// loadBundle loads the embedded locale files into a fresh *i18n.Bundle exactly
// once per process. Subsequent calls return the cached bundle.
//
// The bundle's default language is language.Und so that BOTH shipped locale
// files (en.json, fa.json) load as real translations. go-i18n skips files whose
// tag equals the bundle's default language; using Und avoids that, and the
// locale-sync test guarantees en.json is always complete.
func loadBundle() (*i18n.Bundle, error) {
	bundleOnce.Do(func() {
		bundle = i18n.NewBundle(language.Und)
		entries, err := localeFS.ReadDir("locales")
		if err != nil {
			errBundle = fmt.Errorf("i18n: read locales dir: %w", err)
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if err := loadLocaleFile(e.Name()); err != nil {
				errBundle = err
				return
			}
		}
	})
	return bundle, errBundle
}

// loadLocaleFile reads one embedded locale JSON file, unmarshals it into a map
// of message-id -> i18n.Message, stamps the id onto each message, and adds the
// set to the bundle under the tag parsed from the filename ("en.json" -> en).
//
// We add messages explicitly (rather than via ParseMessageFileBytes) because
// ParseMessageFileBytes only parses; it does not register the messages with the
// bundle.
func loadLocaleFile(name string) error {
	data, err := localeFS.ReadFile("locales/" + name)
	if err != nil {
		return fmt.Errorf("i18n: read locale %s: %w", name, err)
	}
	tag := parseLocaleTag(name)
	var msgs map[string]i18n.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("i18n: parse locale %s: %w", name, err)
	}
	add := make([]*i18n.Message, 0, len(msgs))
	for id, m := range msgs {
		m := m
		m.ID = id
		add = append(add, &m)
	}
	if err := bundle.AddMessages(tag, add...); err != nil {
		return fmt.Errorf("i18n: add messages for %s: %w", name, err)
	}
	return nil
}

// parseLocaleTag extracts the language.Tag from a filename like "en.json" or
// "fa.json". Returns language.Und for unrecognised names.
func parseLocaleTag(name string) language.Tag {
	trim := strings.TrimSuffix(name, ".json")
	return language.Make(trim)
}

// localeCtxKey is the context key for the active locale code.
type localeCtxKey struct{}

// WithLocale returns a derived context carrying the locale code. Middleware
// sets this once per request after resolving Accept-Language / user profile.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeCtxKey{}, locale)
}

// LocaleFromContext returns the active locale code, or DefaultLocale if none.
func LocaleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(localeCtxKey{}).(string); ok && v != "" {
		return v
	}
	return DefaultLocale
}

// ResolveAcceptLanguage maps a raw Accept-Language header value to the closest
// supported locale code (en or fa). An empty or unparseable header yields the
// default locale.
func ResolveAcceptLanguage(header string) string {
	if header == "" {
		return DefaultLocale
	}
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil || len(tags) == 0 {
		return DefaultLocale
	}
	_, idx, _ := matcher.Match(tags[0])
	return supportedTags[idx].String()
}

// supportedCodes is the lowercased locale codes Lahijan ships bundles for.
var supportedCodes = func() map[string]struct{} {
	out := make(map[string]struct{}, len(supportedTags))
	for _, t := range supportedTags {
		out[t.String()] = struct{}{}
	}
	return out
}()

// normalizeLocale maps any locale code onto a supported one, falling back to the
// default when the requested locale has no shipped bundle. This keeps T() from
// returning the raw message id for an unsupported but non-empty locale.
func normalizeLocale(code string) string {
	if _, ok := supportedCodes[code]; ok {
		return code
	}
	return DefaultLocale
}

// Localizer returns a go-i18n Localizer bound to the locale in ctx. The default
// locale (en) is passed as a fallback tag so a message missing from the active
// locale renders in English instead of the raw message id. Callers should not
// cache the result; building a Localizer is cheap.
func Localizer(ctx context.Context) (*i18n.Localizer, error) {
	b, err := loadBundle()
	if err != nil {
		return nil, err
	}
	loc := normalizeLocale(LocaleFromContext(ctx))
	return i18n.NewLocalizer(b, loc, DefaultLocale), nil
}

// T translates the given message id into the locale carried by ctx. args, when
// non-nil, supplies template data for placeholders like {{.Link}}. Missing
// translations fall back to the default locale, and a missing id entirely
// returns the id itself (so a forgotten key is visible instead of crashing the
// request).
func T(ctx context.Context, messageID string, args map[string]any) string {
	loc, err := Localizer(ctx)
	if err != nil {
		return messageID
	}
	cfg := &i18n.LocalizeConfig{MessageID: messageID}
	if args != nil {
		cfg.TemplateData = args
	}
	msg, err := loc.Localize(cfg)
	if err != nil || msg == "" {
		// go-i18n returns an error (or empty string) for a missing id; surface
		// the id rather than an empty string so a forgotten key is obvious.
		return messageID
	}
	return msg
}
