package tools

import (
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.7", "1.14.9", -1},
		{"1.14.9", "1.2.7", 1},
		{"1.5.0", "1.5.0", 0},
		{"v1.5.0", "1.5.0", 0},
		{"1.5.7", "1.5.0", 1},
		{"1.0.0", "2.0.0", -1},
		// Unparseable versions fall through to lexical compare on the failed
		// component. "unknown" > "1" lexically — production never feeds raw
		// "unknown" here (callers special-case it first), so the ordering is
		// asserted only to lock down behaviour.
		{"unknown", "1.5.0", 1},
	}
	for _, tc := range tests {
		got := compareSemver(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNormalizeSeverity(t *testing.T) {
	cases := map[string]string{
		"LOW":      "low",
		"MODERATE": "medium",
		"MEDIUM":   "medium",
		"HIGH":     "high",
		"CRITICAL": "critical",
		"":         "unknown",
		"weird":    "unknown",
		" high ":   "high",
	}
	for in, want := range cases {
		if got := normalizeSeverity(in); got != want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpgradeComplexity(t *testing.T) {
	tests := []struct {
		name      string
		resources int
		behind    int
		majorJump bool
		want      string
	}{
		{"low_few_resources_close_version", 5, 1, false, "Low"},
		{"medium_more_resources", 30, 0, false, "Medium"},
		{"medium_many_minors_behind", 5, 3, false, "Medium"},
		{"high_many_resources", 60, 0, false, "High"},
		{"high_many_minors_behind", 5, 8, false, "High"},
		{"high_major_jump", 5, 1, true, "High"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := upgradeComplexity(tc.resources, tc.behind, tc.majorJump)
			if got != tc.want {
				t.Fatalf("upgradeComplexity(%d, %d, %v) = %q, want %q",
					tc.resources, tc.behind, tc.majorJump, got, tc.want)
			}
		})
	}
}

func TestVersionsBehind(t *testing.T) {
	tests := []struct {
		current, latest string
		want            int
	}{
		{"1.2.7", "1.14.9", 12},
		{"1.14.9", "1.14.9", 0},
		{"1.5.7", "1.14.9", 9},
		{"unknown", "1.14.9", 0},
		{"1.20.0", "1.14.9", 0}, // future versions clamp to 0
	}
	for _, tc := range tests {
		if got := versionsBehind(tc.current, tc.latest); got != tc.want {
			t.Errorf("versionsBehind(%q, %q) = %d, want %d", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestParseOSVResponse_RealCVEShape(t *testing.T) {
	// Shape mirrors the real OSV.dev /v1/query response for terraform 1.2.7,
	// which returns CVE-2023-4782 (arbitrary file write, fixed in 1.5.7).
	body := []byte(`{
		"vulns": [
			{
				"id": "GHSA-h626-pv66-hhm7",
				"summary": "Terraform allows arbitrary file write during the init operation",
				"aliases": ["CVE-2023-4782", "GO-2023-2055"],
				"database_specific": {"severity": "MODERATE"},
				"affected": [
					{
						"ranges": [
							{"events": [{"introduced": "1.0.8"}, {"fixed": "1.5.7"}]}
						]
					}
				]
			}
		]
	}`)

	entries, parseErr := parseOSVResponse(body)
	if parseErr {
		t.Fatalf("parseOSVResponse reported error on valid body")
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.ID != "CVE-2023-4782" {
		t.Errorf("ID = %q, want CVE alias to be preferred", got.ID)
	}
	if got.Severity != "medium" {
		t.Errorf("Severity = %q, want medium", got.Severity)
	}
	if got.FixedIn != "1.5.7" {
		t.Errorf("FixedIn = %q, want 1.5.7", got.FixedIn)
	}
	if got.Summary == "" {
		t.Errorf("Summary should be populated")
	}
}

func TestParseOSVResponse_GarbageBody(t *testing.T) {
	if _, parseErr := parseOSVResponse([]byte("not json")); !parseErr {
		t.Errorf("expected parse error on garbage body")
	}
}

func TestParseOSVResponse_NoVulns(t *testing.T) {
	entries, parseErr := parseOSVResponse([]byte(`{"vulns": []}`))
	if parseErr {
		t.Fatalf("unexpected parse error on empty vulns")
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestParseOSVResponse_DedupsByID(t *testing.T) {
	// OSV often returns both the GHSA and GO-* records for the same CVE.
	// The aliases field aligns them on the same canonical CVE id; we should
	// emit only one entry.
	body := []byte(`{
		"vulns": [
			{"id": "GHSA-aaaa", "summary": "first", "aliases": ["CVE-2024-9999"],
			 "database_specific": {"severity": "HIGH"},
			 "affected": [{"ranges": [{"events": [{"fixed": "1.0.0"}]}]}]},
			{"id": "GO-2024-0001", "summary": "duplicate", "aliases": ["CVE-2024-9999"],
			 "database_specific": {"severity": "HIGH"},
			 "affected": [{"ranges": [{"events": [{"fixed": "1.0.0"}]}]}]}
		]
	}`)
	entries, parseErr := parseOSVResponse(body)
	if parseErr {
		t.Fatalf("unexpected parse error")
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries after dedup, want 1", len(entries))
	}
}
