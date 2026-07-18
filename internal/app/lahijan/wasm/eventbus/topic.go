// Package eventbus: topic.go defines the topic-pattern matcher. The
// algorithm is identical to the one in wasm/permission (permission.Allowed)
// so a manifest grant of "events.listen:dns.record.*" satisfies the same
// shape as the bus's subscription pattern "dns.record.*".
package eventbus

import "strings"

// TopicPattern is a string with the same wildcard semantics as a
// permission qualifier suffix: a trailing ".*" matches every dotted
// child of the prefix. Mid-token "*" and "scope.*" wildcards are NOT
// supported — the wildcard must be the LAST segment and must follow
// (or be) a dotted component.
type TopicPattern string

// Matches reports whether the topic falls under the pattern. Examples:
//
//	pattern "dns.record.created", topic "dns.record.created" -> true
//	pattern "dns.record.*",       topic "dns.record.created" -> true
//	pattern "dns.record.*",       topic "dns.record"         -> false
//	pattern "dns.record.*",       topic "dns.zone.created"   -> false
//	pattern "*",                  topic "anything"           -> true
//	pattern "*",                  topic "dns.record.created" -> true (bare * = match-all)
//	pattern "dns.*",              topic "dns.record.created" -> false
//
// The bare "*" pattern is special-cased to match every topic so the
// EventService can register a single listener that receives every
// event for downstream dispatch to per-plugin subscriptions.
//
// "scope.*" (a single segment followed by ".*") matches only direct
// children (dns.record) — not grand-children. This matches the
// permission slug semantics so a grant the admin sees is the grant the
// plugin gets.
func (p TopicPattern) Matches(topic string) bool {
	pat := string(p)
	if pat == "" {
		return false
	}
	// Bare "*" matches every topic.
	if pat == "*" {
		return true
	}
	// Exact match.
	if pat == topic {
		return true
	}
	// "prefix.*" wildcard: matches "prefix.<single-segment>".
	if !strings.HasSuffix(pat, ".*") {
		return false
	}
	head := strings.TrimSuffix(pat, "*") // "prefix."
	// The topic must start with head AND have a non-empty suffix that
	// contains no further dots. dns.record.* -> head "dns.record.";
	// topic "dns.record.created" matches (head + "created"). topic
	// "dns.record.a.b" does NOT match (dots in the suffix).
	if !strings.HasPrefix(topic, head) {
		return false
	}
	suffix := topic[len(head):]
	if suffix == "" {
		return false
	}
	return !strings.Contains(suffix, ".")
}

// MatchSubscription is a typed alias for the (pattern, topic) check the
// bus runs at emit time. Exposed so the EventService and host-function
// subscription paths can share the same algorithm without re-deriving.
func MatchSubscription(pattern, topic string) bool {
	return TopicPattern(pattern).Matches(topic)
}
