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
