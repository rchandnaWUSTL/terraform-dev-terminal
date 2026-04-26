package tools

import (
	"strings"
	"testing"
)

func TestCategorizeFailure(t *testing.T) {
	tests := []struct {
		name           string
		planLogs       string
		applyLogs      string
		wantCategory   string
		wantSnippetHas string
		wantResource   string
	}{
		{
			name: "auth_accessdenied",
			planLogs: `Terraform v1.14.9
{"@level":"info","@message":"aws_vpc.main: Plan to create","@module":"terraform.ui"}
Error: creating VPC: operation error EC2: CreateVpc, https response error StatusCode: 403, AccessDenied: User arn:aws:iam::123:user/x is not authorized to perform ec2:CreateVpc`,
			applyLogs:      "",
			wantCategory:   "auth",
			wantSnippetHas: "AccessDenied",
			wantResource:   "aws_vpc.main",
		},
		{
			name:         "quota_limit_exceeded",
			planLogs:     "Error: creating subnet: ServiceQuotaExceeded: The maximum number of VPCs has been reached.",
			applyLogs:    "",
			wantCategory: "quota",
		},
		{
			name:         "resource_conflict_already_exists",
			planLogs:     "Error: creating bucket: BucketAlreadyOwnedByYou: Your previous request to create the named bucket succeeded already exists.",
			applyLogs:    "",
			wantCategory: "resource_conflict",
		},
		{
			name:         "network_timeout",
			planLogs:     "Error: dial tcp 10.0.0.1:443: i/o timeout",
			applyLogs:    "",
			wantCategory: "network",
		},
		{
			name:         "provider_plugin_failure",
			planLogs:     "Error: Failed to install provider aws: checksum mismatch on plugin tarball",
			applyLogs:    "",
			wantCategory: "provider",
		},
		{
			name:         "config_syntax_error",
			planLogs:     "Error: Invalid configuration: unsupported argument \"fooo\" in resource block",
			applyLogs:    "",
			wantCategory: "config",
		},
		{
			name:         "policy_sentinel_violation",
			planLogs:     "Sentinel Result: false\npolicy check failed: require-tags rejected because resource missing tag owner",
			applyLogs:    "",
			wantCategory: "policy",
		},
		{
			name: "ordering_policy_beats_auth",
			// Both a policy failure and "not authorized" substring present — policy
			// must win because it is checked first (failed policies routinely include
			// the word "denied" in their messages).
			planLogs:     "policy check failed: prod-guardrails denied the run because the principal is not authorized to modify production.",
			applyLogs:    "",
			wantCategory: "policy",
		},
		{
			name:         "unknown_fallback",
			planLogs:     "Terraform completed with a totally novel error the heuristics have never seen.",
			applyLogs:    "",
			wantCategory: "unknown",
		},
		{
			name:     "apply_logs_preferred_for_snippet",
			planLogs: "plan completed cleanly.",
			applyLogs: `{"@level":"info","@message":"aws_s3_bucket.state: Creating...","@module":"terraform.ui"}
Error: creating S3 bucket: AccessDenied: not authorized to perform s3:CreateBucket`,
			wantCategory:   "auth",
			wantSnippetHas: "AccessDenied",
			wantResource:   "aws_s3_bucket.state",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := categorizeFailure(tc.planLogs, tc.applyLogs)
			if d.category != tc.wantCategory {
				t.Fatalf("category: got %q, want %q", d.category, tc.wantCategory)
			}
			if d.summary == "" || d.fix == "" {
				t.Fatalf("summary/fix must be non-empty for category %q", d.category)
			}
			if tc.wantSnippetHas != "" && !strings.Contains(d.snippet, tc.wantSnippetHas) {
				t.Fatalf("snippet missing %q; got: %q", tc.wantSnippetHas, d.snippet)
			}
			if tc.wantResource != "" {
				found := false
				for _, r := range d.resources {
					if r == tc.wantResource {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("affected resource %q not found; got %v", tc.wantResource, d.resources)
				}
			}
		})
	}
}

func TestCategorizeFailureAuthFixMentionsCredentials(t *testing.T) {
	d := categorizeFailure("Error: AccessDenied on ec2:CreateVpc", "")
	if d.category != "auth" {
		t.Fatalf("category: got %q, want auth", d.category)
	}
	if !strings.Contains(strings.ToLower(d.fix), "credentials") {
		t.Fatalf("auth fix must mention credentials; got: %q", d.fix)
	}
}

func TestCategorizeFailurePolicyFixMentionsPolicyCheck(t *testing.T) {
	d := categorizeFailure("policy check failed: require-tags", "")
	if d.category != "policy" {
		t.Fatalf("category: got %q, want policy", d.category)
	}
	if !strings.Contains(d.fix, "_hcp_tf_policy_check") {
		t.Fatalf("policy fix must mention _hcp_tf_policy_check; got: %q", d.fix)
	}
}

func TestDecodeLogsField(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"happy_path", []byte(`{"logs":"hello","plan_id":"plan-x"}`), "hello"},
		{"empty_payload", nil, ""},
		{"invalid_json", []byte(`not json`), ""},
		{"missing_logs_field", []byte(`{"plan_id":"plan-x"}`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeLogsField(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrimLineJSONWrapper(t *testing.T) {
	raw := `{"@level":"error","@message":"Error: AccessDenied on ec2:CreateVpc","@module":"terraform.ui"}`
	got := trimLine(raw)
	want := "Error: AccessDenied on ec2:CreateVpc"
	if got != want {
		t.Fatalf("trimLine: got %q, want %q", got, want)
	}
	plain := "Error: plain text"
	if trimLine(plain) != plain {
		t.Fatalf("plain line must pass through unchanged")
	}
}

func TestExtractResourcesRejectsEmailsAndFilenames(t *testing.T) {
	// Real log line that leaked through initial implementation: an email and a
	// .tf filename both look like resource addresses to a naive regex.
	line := `Error: creating S3 Bucket: not authorized: arn:aws:sts::650169680785:assumed-role/aws_roshan.chandna_test-developer/roshan.chandna@hashicorp.com cannot access main.tf; aws_s3_bucket.state_artifacts was being created`
	got := extractResources(line, line)
	for _, r := range got {
		if strings.HasSuffix(r, ".tf") || strings.Contains(r, "chandna") {
			t.Fatalf("filter leak: %v", got)
		}
	}
	found := false
	for _, r := range got {
		if r == "aws_s3_bucket.state_artifacts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected legitimate aws_s3_bucket.state_artifacts in %v", got)
	}
}

func TestInterpretPolicyName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"terraform_version_alias", "allowed-terraform-version", "Upgrade your Terraform version"},
		{"terraform_version_short", "terraform-version", "Upgrade your Terraform version"},
		{"ssh_restrict", "restrict-ssh", "Remove SSH (port 22) access from security groups"},
		{"ssh_no", "no-ssh-22", "Remove SSH (port 22) access from security groups"},
		{"required_tags", "required-tags", "Add required tags to all resources"},
		{"enforce_tags", "enforce-tags-on-aws", "Add required tags to all resources"},
		{"allowed_regions", "allowed-regions", "Move resources to an approved AWS region"},
		{"restrict_regions", "restrict-regions", "Move resources to an approved AWS region"},
		{"cost_limit", "cost-limit", "Reduce estimated monthly cost below the policy threshold"},
		{"budget", "monthly-budget", "Reduce estimated monthly cost below the policy threshold"},
		{"case_insensitive", "Required-Tags-PROD", "Add required tags to all resources"},
		{"unknown_falls_back", "prod-guardrails-xyz", policyDefaultRequirement},
		{"empty_falls_back", "", policyDefaultRequirement},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := interpretPolicyName(tc.in)
			if got != tc.want {
				t.Fatalf("interpretPolicyName(%q): got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDecodeFailedPolicies(t *testing.T) {
	raw := []byte(`[
		{"name":"required-tags","status":"failed","enforcement_level":"hard-mandatory"},
		{"name":"cost-limit","status":"hard_failed","EnforcementLevel":"soft-mandatory"},
		{"name":"allowed-regions","status":"passed","enforcement_level":"hard-mandatory"},
		{"name":"opaque-policy","status":"errored"}
	]`)
	got := decodeFailedPolicies(raw)
	if len(got) != 3 {
		t.Fatalf("expected 3 failed entries, got %d: %+v", len(got), got)
	}
	wantNames := map[string]string{
		"required-tags":  "hard-mandatory",
		"cost-limit":     "soft-mandatory",
		"opaque-policy":  "",
	}
	for _, fp := range got {
		want, ok := wantNames[fp.name]
		if !ok {
			t.Fatalf("unexpected policy name in result: %q", fp.name)
		}
		if fp.enforcementLevel != want {
			t.Fatalf("%s enforcement: got %q want %q", fp.name, fp.enforcementLevel, want)
		}
	}
}

func TestDecodeFailedPoliciesEmpty(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(``),
		[]byte(`[]`),
		[]byte(`[{"name":"required-tags","status":"passed"}]`),
		[]byte(`not json`),
	}
	for i, raw := range cases {
		if got := decodeFailedPolicies(raw); got != nil {
			t.Fatalf("case %d: expected nil, got %+v", i, got)
		}
	}
}

func TestInterpretFailedPolicies(t *testing.T) {
	raw := []byte(`[
		{"name":"required-tags-prod","status":"failed","enforcement_level":"hard-mandatory"},
		{"name":"weird-internal-policy","status":"failed"}
	]`)
	got := interpretFailedPolicies(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0]["policy_name"] != "required-tags-prod" {
		t.Fatalf("entry 0 name: %v", got[0]["policy_name"])
	}
	if got[0]["enforcement_level"] != "hard-mandatory" {
		t.Fatalf("entry 0 enforcement_level: %v", got[0]["enforcement_level"])
	}
	if got[0]["requirement"] != "Add required tags to all resources" {
		t.Fatalf("entry 0 requirement: %v", got[0]["requirement"])
	}
	if _, ok := got[1]["enforcement_level"]; ok {
		t.Fatalf("missing enforcement level should be omitted, got %v", got[1])
	}
	if got[1]["requirement"] != policyDefaultRequirement {
		t.Fatalf("entry 1 should fall back to default; got %v", got[1]["requirement"])
	}
}

func TestExtractResourcesFiltersNonResourceTokens(t *testing.T) {
	line := `{"@module":"terraform.ui","@message":"aws_vpc.main: creating via registry.terraform.io/hashicorp/aws"}`
	got := extractResources(line, line)
	for _, r := range got {
		if strings.HasPrefix(r, "registry.") || strings.HasPrefix(r, "terraform.") {
			t.Fatalf("unexpected non-resource token in output: %v", got)
		}
	}
	found := false
	for _, r := range got {
		if r == "aws_vpc.main" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected aws_vpc.main in %v", got)
	}
}
