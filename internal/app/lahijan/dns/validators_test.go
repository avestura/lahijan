// validators_test.go covers the per-type record content validators. The
// validators are the WS-15 DoD item "invalid record content rejected with
// a clear message (per type)"; this test asserts every supported type
// has both an accept and a reject case.
package dns

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateTTL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ttl  int
		want error
	}{
		{"zero", 0, ErrInvalidTTL},
		{"below min", 299, ErrInvalidTTL},
		{"at min", MinTTL, nil},
		{"default", 3600, nil},
		{"at max", MaxTTL, nil},
		{"above max", MaxTTL + 1, ErrInvalidTTL},
		{"negative", -1, ErrInvalidTTL},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTTL(tc.ttl)
			if tc.want == nil {
				if err != nil {
					t.Errorf("ValidateTTL(%d) = %v; want nil", tc.ttl, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("ValidateTTL(%d) = %v; want %v", tc.ttl, err, tc.want)
			}
		})
	}
}

func TestIsSupportedRecordType(t *testing.T) {
	t.Parallel()
	for _, rt := range SupportedRecordTypes {
		if !IsSupportedRecordType(rt) {
			t.Errorf("expected %q to be supported", rt)
		}
	}
	if IsSupportedRecordType("BOGUS") {
		t.Errorf("expected BOGUS to be unsupported")
	}
}

func TestValidateRecordContent(t *testing.T) {
	t.Parallel()
	const zone = "example.com."
	cases := []struct {
		name        string
		rtype       string
		content     string
		wantErr     error
		wantMessage string
	}{
		// A
		{"A valid", TypeA, "192.0.2.1", nil, ""},
		{"A invalid", TypeA, "not.an.ip", ErrInvalidRecordContent, "IPv4"},
		{"A v6 in v4", TypeA, "::1", ErrInvalidRecordContent, "IPv4"},
		// AAAA
		{"AAAA valid", TypeAAAA, "2001:db8::1", nil, ""},
		{"AAAA invalid", TypeAAAA, "not.an.ip", ErrInvalidRecordContent, "IPv6"},
		{"AAAA v4 in v6", TypeAAAA, "192.0.2.1", ErrInvalidRecordContent, "IPv6"},
		// CNAME
		{"CNAME valid", TypeCNAME, "target.example.org.", nil, ""},
		{"CNAME no dot", TypeCNAME, "target.example.org", ErrInvalidRecordContent, "dot"},
		{"CNAME uppercase", TypeCNAME, "Target.example.org.", ErrInvalidRecordContent, "lowercase"},
		// NS
		{"NS valid", TypeNS, "ns1.example.com.", nil, ""},
		{"NS no dot", TypeNS, "ns1.example.com", ErrInvalidRecordContent, "dot"},
		// PTR
		{"PTR valid", TypePTR, "host.example.com.", nil, ""},
		{"PTR no dot", TypePTR, "host.example.com", ErrInvalidRecordContent, "dot"},
		// MX
		{"MX valid", TypeMX, "10 mail.example.com.", nil, ""},
		{"MX missing prio", TypeMX, "mail.example.com.", ErrInvalidRecordContent, "priority"},
		{"MX prio too large", TypeMX, "70000 mail.example.com.", ErrInvalidRecordContent, "priority"},
		{"MX target no dot", TypeMX, "10 mail.example.com", ErrInvalidRecordContent, "MX target"},
		// SRV
		{"SRV valid", TypeSRV, "10 60 5060 sipserver.example.com.", nil, ""},
		{"SRV too few fields", TypeSRV, "10 60 5060", ErrInvalidRecordContent, "target"},
		{"SRV port too large", TypeSRV, "10 60 99999 sipserver.example.com.", ErrInvalidRecordContent, "port"},
		// CAA
		{"CAA valid", TypeCAA, "0 issue \"letsencrypt.org\"", nil, ""},
		{"CAA missing value", TypeCAA, "0 issue", ErrInvalidRecordContent, "value"},
		{"CAA flags too large", TypeCAA, "300 issue \"letsencrypt.org\"", ErrInvalidRecordContent, "flags"},
		// TXT
		{"TXT valid", TypeTXT, "\"v=spf1 -all\"", nil, ""},
		{"TXT unquoted", TypeTXT, "v=spf1 -all", ErrInvalidRecordContent, "quoted"},
		{"TXT empty", TypeTXT, "", ErrInvalidRecordContent, "empty"},
		// DS
		{"DS valid", TypeDS, "12345 13 2 abcd1234abcd1234", nil, ""},
		{"DS odd digest", TypeDS, "12345 13 2 abc", ErrInvalidRecordContent, "even-length"},
		{"DS non-hex digest", TypeDS, "12345 13 2 zzzz", ErrInvalidRecordContent, "hex"},
		// TLSA
		{"TLSA valid", TypeTLSA, "3 1 1 abc123def456", nil, ""},
		{"TLSA too few fields", TypeTLSA, "3 1 1", ErrInvalidRecordContent, "certificate"},
		// SOA
		{"SOA valid", TypeSOA, "ns1.example.com. hostmaster.example.com. 2024010101 10800 3600 604800 3600", nil, ""},
		{"SOA too few fields", TypeSOA, "ns1.example.com. hostmaster.example.com. 1", ErrInvalidRecordContent, "minimum"},
		// Unsupported
		{"unsupported", "BOGUS", "anything", ErrInvalidRecordType, "unsupported"},
		// Empty content for valid type
		{"empty A", TypeA, "", ErrInvalidRecordContent, "empty"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRecordContent(tc.rtype, tc.content, zone)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateRecordContent(%s, %q) = %v; want nil", tc.rtype, tc.content, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("ValidateRecordContent(%s, %q) = %v; want %v", tc.rtype, tc.content, err, tc.wantErr)
				return
			}
			if tc.wantMessage != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantMessage)) {
				t.Errorf("ValidateRecordContent(%s, %q) error %q does not contain %q", tc.rtype, tc.content, err.Error(), tc.wantMessage)
			}
		})
	}
}

func TestValidateCanonicalZoneName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"valid", "example.com.", nil},
		{"empty", "", ErrInvalidZoneName},
		{"no trailing dot", "example.com", ErrInvalidZoneName},
		{"uppercase", "Example.com.", ErrInvalidZoneName},
		{"whitespace", "ex ample.com.", ErrInvalidZoneName},
		{"subdomain valid", "tenant.example.com.", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateCanonicalZoneName(tc.in)
			if tc.want == nil {
				if err != nil {
					t.Errorf("validateCanonicalZoneName(%q) = %v; want nil", tc.in, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("validateCanonicalZoneName(%q) = %v; want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestValidateRecordName(t *testing.T) {
	t.Parallel()
	const zone = "example.com."
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"apex", "example.com.", nil},
		{"subdomain", "www.example.com.", nil},
		{"deep subdomain", "api.v2.example.com.", nil},
		{"empty", "", ErrInvalidRecordName},
		{"no dot", "www.example.com", ErrInvalidRecordName},
		{"uppercase", "Www.example.com.", ErrInvalidRecordName},
		{"whitespace", "ww w.example.com.", ErrInvalidRecordName},
		{"out of zone", "www.other.com.", ErrInvalidRecordName},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRecordName(tc.in, zone)
			if tc.want == nil {
				if err != nil {
					t.Errorf("validateRecordName(%q, %q) = %v; want nil", tc.in, zone, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("validateRecordName(%q, %q) = %v; want %v", tc.in, zone, err, tc.want)
			}
		})
	}
}

func TestParseMXPriority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int32
	}{
		{"10 mail.example.com.", 10},
		{"0 mail.example.com.", 0},
		{"65535 mail.example.com.", 65535},
		{"notanint mail.example.com.", 0},
		{"", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := ParseMXPriority(tc.in)
			if got != tc.want {
				t.Errorf("ParseMXPriority(%q) = %d; want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeIP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"192.0.2.1", "192.0.2.1"},
		{"2001:db8::1", "2001:db8::1"},
		{"2001:0db8:0000::1", "2001:db8::1"},
		{"not-an-ip", "not-an-ip"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := CanonicalizeIP(tc.in)
			if got != tc.want {
				t.Errorf("CanonicalizeIP(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGetTemplate(t *testing.T) {
	t.Parallel()
	tpl, err := GetTemplate("google-workspace")
	if err != nil {
		t.Fatalf("GetTemplate(google-workspace) = %v; want nil", err)
	}
	if len(tpl.Records) == 0 {
		t.Errorf("google-workspace template has no records")
	}
	if _, err := GetTemplate("bogus"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("GetTemplate(bogus) = %v; want ErrTemplateNotFound", err)
	}
}

func TestExpandTemplatePlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		zone string
		want string
	}{
		{"%s", "example.com.", "example.com."},
		{"mail.%s", "example.com.", "mail.example.com."},
		{"10 %w.mail.protection.outlook.com.", "example.com.", "10 example.com.mail.protection.outlook.com."},
		{"no placeholder", "example.com.", "no placeholder"},
		{"multi %s %s", "example.com.", "multi example.com. example.com."},
		{"mixed %s %w", "example.com.", "mixed example.com. example.com"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := expandTemplatePlaceholder(tc.in, tc.zone)
			if got != tc.want {
				t.Errorf("expandTemplatePlaceholder(%q, %q) = %q; want %q", tc.in, tc.zone, got, tc.want)
			}
		})
	}
}
