package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// driftDetectCall implements _hcp_tf_drift_detect by hitting the JSON:API
// endpoint GET /api/v2/workspaces/<id>/current-assessment-result. The previous
// implementation shelled out to `hcptf assessmentresult list`, which routes to
// a non-existent endpoint and always 404s.
//
// Flow:
//  1. `hcptf workspace read` to resolve workspace name → workspace ID.
//  2. Bearer token via readTFCToken().
//  3. GET current-assessment-result. A 404 means the workspace does not have
//     health assessments enabled, which is a normal state (not an error) — we
//     return assessments_enabled=false so the model can explain that.
//  4. When the assessment reports drift, fetch data.links.json-output and
//     extract every resource_change whose actions list is anything other than
//     ["no-op"] — those addresses are the drifted resources.
func driftDetectCall(ctx context.Context, args map[string]string, timeoutSec int) *CallResult {
	start := time.Now()
	result := &CallResult{ToolName: "_hcp_tf_drift_detect", Args: args}

	if err := require(args, "org", "workspace"); err != nil {
		result.Err = &ToolError{ErrorCode: "invalid_tool", Message: err.Error()}
		result.Duration = time.Since(start)
		return result
	}
	org := args["org"]
	workspace := args["workspace"]

	wsRaw, ferr := fetchHCPTFJSON(ctx, timeoutSec, "workspace", "read",
		"-org="+org, "-name="+workspace, "-output=json")
	if ferr != nil {
		result.Err = ferr
		result.Duration = time.Since(start)
		return result
	}
	var wsDetail map[string]any
	if err := json.Unmarshal(wsRaw, &wsDetail); err != nil {
		result.Err = &ToolError{ErrorCode: "marshal_error", Message: "decode workspace read: " + err.Error()}
		result.Duration = time.Since(start)
		return result
	}
	wsID := firstStringField(wsDetail, "ID", "id")
	if wsID == "" {
		result.Err = &ToolError{ErrorCode: "execution_error", Message: "workspace read did not return a workspace ID"}
		result.Duration = time.Since(start)
		return result
	}

	token := readTFCToken()
	if token == "" {
		result.Err = &ToolError{
			ErrorCode: "execution_error",
			Message:   "no Terraform Cloud token in ~/.terraform.d/credentials.tfrc.json — run `terraform login` to authenticate",
			Retryable: false,
		}
		result.Duration = time.Since(start)
		return result
	}

	assessment, derr := fetchCurrentAssessment(ctx, token, wsID, timeoutSec)
	if derr != nil {
		result.Err = derr
		result.Duration = time.Since(start)
		return result
	}

	// 404 — the workspace has assessments disabled or none has run yet.
	if assessment == nil {
		payload := map[string]any{
			"workspace":           workspace,
			"org":                 org,
			"assessments_enabled": false,
			"message":             "No health assessment is available for this workspace. Enable health assessments in the workspace settings, or wait for the next scheduled assessment to run.",
		}
		out, _ := json.Marshal(payload)
		result.Output = json.RawMessage(out)
		result.Duration = time.Since(start)
		return result
	}

	attrs, _ := assessment["attributes"].(map[string]any)
	links, _ := assessment["links"].(map[string]any)

	drifted := boolField(attrs, "drifted")
	resourcesDrifted := intField(attrs, "resources-drifted")
	resourcesUndrifted := intField(attrs, "resources-undrifted")
	succeeded := boolField(attrs, "succeeded")
	createdAt := stringField(attrs, "created-at")
	errMsg := stringField(attrs, "error-message")
	asmtID := stringField(assessment, "id")

	driftedAddresses := []string{}
	if drifted && resourcesDrifted > 0 {
		jsonOutPath := stringField(links, "json-output")
		if jsonOutPath != "" {
			driftedAddresses = fetchDriftedAddresses(ctx, token, jsonOutPath, timeoutSec)
		}
	}

	payload := map[string]any{
		"workspace":           workspace,
		"org":                 org,
		"assessments_enabled": true,
		"assessment_id":       asmtID,
		"drifted":             drifted,
		"succeeded":           succeeded,
		"last_assessment_at":  createdAt,
		"resources_drifted":   resourcesDrifted,
		"resources_undrifted": resourcesUndrifted,
		"drifted_addresses":   driftedAddresses,
		"error_message":       errMsg,
	}
	out, mErr := json.Marshal(payload)
	if mErr != nil {
		result.Err = &ToolError{ErrorCode: "marshal_error", Message: mErr.Error()}
		result.Duration = time.Since(start)
		return result
	}
	result.Output = json.RawMessage(out)
	result.Duration = time.Since(start)
	return result
}

// fetchCurrentAssessment GETs the workspace's current assessment result.
// Returns (nil, nil) when the API responds with 404 — that means health
// assessments are not enabled for the workspace.
func fetchCurrentAssessment(ctx context.Context, token, wsID string, timeoutSec int) (map[string]any, *ToolError) {
	url := fmt.Sprintf("https://app.terraform.io/api/v2/workspaces/%s/current-assessment-result", wsID)
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &ToolError{ErrorCode: "execution_error", Message: "build assessment request: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("User-Agent", "tfpilot")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &ToolError{ErrorCode: "execution_error", Message: "fetch assessment: " + err.Error(), Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &ToolError{
			ErrorCode: "execution_error",
			Message:   fmt.Sprintf("assessment endpoint returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 200)),
			Retryable: resp.StatusCode >= 500,
		}
	}
	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, &ToolError{ErrorCode: "execution_error", Message: "read assessment body: " + rerr.Error()}
	}
	var doc struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, &ToolError{ErrorCode: "marshal_error", Message: "decode assessment: " + err.Error()}
	}
	return doc.Data, nil
}

// fetchDriftedAddresses pulls the assessment's json-output (a Terraform plan
// JSON) and returns the resource addresses whose change.actions is anything
// other than ["no-op"]. Best-effort: returns an empty slice if the fetch or
// parse fails — the caller still reports the high-level drift counts.
func fetchDriftedAddresses(ctx context.Context, token, jsonOutPath string, timeoutSec int) []string {
	url := "https://app.terraform.io" + jsonOutPath
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return []string{}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "tfpilot")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return []string{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return []string{}
	}
	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return []string{}
	}
	var plan struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(body, &plan); err != nil {
		return []string{}
	}
	out := []string{}
	for _, rc := range plan.ResourceChanges {
		if isDriftAction(rc.Change.Actions) && rc.Address != "" {
			out = append(out, rc.Address)
		}
	}
	sort.Strings(out)
	return out
}

func isDriftAction(actions []string) bool {
	if len(actions) == 0 {
		return false
	}
	for _, a := range actions {
		if a != "no-op" {
			return true
		}
	}
	return false
}

func boolField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func intField(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
