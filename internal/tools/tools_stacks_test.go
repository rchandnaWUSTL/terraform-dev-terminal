package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStackVsWorkspaceRecommendation(t *testing.T) {
	tests := []struct {
		name            string
		useCase         string
		wantRecommend   string
		wantKeywordHit  string
		wantLimitations bool
	}{
		{
			name:            "multi_region_suggests_stack",
			useCase:         "deploy to 3 regions",
			wantRecommend:   "stack",
			wantKeywordHit:  "region",
			wantLimitations: true,
		},
		{
			name:            "multi_region_literal",
			useCase:         "we run a multi-region service",
			wantRecommend:   "stack",
			wantKeywordHit:  "multi-region",
			wantLimitations: true,
		},
		{
			name:            "sentinel_forces_workspace",
			useCase:         "I need Sentinel policies on every run",
			wantRecommend:   "workspace",
			wantKeywordHit:  "sentinel",
			wantLimitations: true,
		},
		{
			name:            "drift_detection_forces_workspace",
			useCase:         "I need drift detection nightly",
			wantRecommend:   "workspace",
			wantKeywordHit:  "drift",
			wantLimitations: true,
		},
		{
			name:            "kubernetes_suggests_stack",
			useCase:         "deploying kubernetes across clusters",
			wantRecommend:   "stack",
			wantKeywordHit:  "kubernetes",
			wantLimitations: true,
		},
		{
			name:            "simple_module_suggests_workspace",
			useCase:         "just a simple module for one bucket",
			wantRecommend:   "workspace",
			wantKeywordHit:  "simple",
			wantLimitations: true,
		},
		{
			name:            "both_signals_workspace_wins",
			useCase:         "scale across regions but need Sentinel policy",
			wantRecommend:   "workspace",
			wantKeywordHit:  "policy",
			wantLimitations: true,
		},
		{
			name:            "neutral_returns_either",
			useCase:         "what time is it",
			wantRecommend:   "either",
			wantLimitations: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := stackVsWorkspaceCall(context.Background(), map[string]string{
				"org":      "sarah-test-org",
				"use_case": tc.useCase,
			}, 5)
			if res.Err != nil {
				t.Fatalf("unexpected error: %+v", res.Err)
			}
			var payload map[string]any
			if err := json.Unmarshal(res.Output, &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, _ := payload["recommendation"].(string)
			if got != tc.wantRecommend {
				t.Fatalf("recommendation: got %q want %q", got, tc.wantRecommend)
			}
			if tc.wantKeywordHit != "" {
				reasoning, _ := payload["reasoning"].(string)
				if !strings.Contains(strings.ToLower(reasoning), tc.wantKeywordHit) {
					t.Errorf("reasoning missing keyword %q: %q", tc.wantKeywordHit, reasoning)
				}
			}
			if tc.wantLimitations {
				lims, ok := payload["key_limitations"].([]any)
				if !ok || len(lims) < 4 {
					t.Fatalf("key_limitations: expected at least 4 entries, got %v", payload["key_limitations"])
				}
				want := []string{
					"Stacks do not support policy as code (Sentinel/OPA)",
					"Stacks do not support drift detection",
					"Stacks do not support run tasks",
					"Maximum 20 deployments per stack",
				}
				for _, w := range want {
					found := false
					for _, l := range lims {
						if s, _ := l.(string); s == w {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("key_limitations missing %q", w)
					}
				}
			}
			for _, key := range []string{"use_stack_when", "use_workspace_when"} {
				arr, ok := payload[key].([]any)
				if !ok || len(arr) == 0 {
					t.Errorf("%s: expected non-empty list, got %v", key, payload[key])
				}
			}
		})
	}
}

func TestStackVsWorkspaceRequiresArgs(t *testing.T) {
	res := stackVsWorkspaceCall(context.Background(), map[string]string{"org": "x"}, 5)
	if res.Err == nil || res.Err.ErrorCode != "invalid_tool" {
		t.Fatalf("expected invalid_tool error, got %+v", res.Err)
	}
}

func TestStackHealth(t *testing.T) {
	cases := []struct {
		name        string
		deployments []map[string]any
		want        string
	}{
		{"empty_is_unknown", nil, "Unknown"},
		{"all_applied_is_healthy", []map[string]any{
			{"status": "applied"},
			{"status": "applied"},
		}, "Healthy"},
		{"any_errored_is_degraded", []map[string]any{
			{"status": "applied"},
			{"status": "errored"},
		}, "Degraded"},
		{"mixed_non_applied_is_unknown", []map[string]any{
			{"status": "applied"},
			{"status": "planning"},
		}, "Unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stackHealth(tc.deployments); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
