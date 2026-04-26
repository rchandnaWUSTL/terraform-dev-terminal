package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// workspaceDependenciesCall implements _hcp_tf_workspace_dependencies. It
// downloads workspace state JSON, walks resources for terraform_remote_state
// data sources, and builds a cross-workspace dependency graph. With a
// `workspace` arg it returns single-workspace depends_on/depended_by; without
// it, returns the full org-wide graph.
func workspaceDependenciesCall(ctx context.Context, args map[string]string, timeoutSec int) *CallResult {
	start := time.Now()
	result := &CallResult{ToolName: "_hcp_tf_workspace_dependencies", Args: args}

	if err := require(args, "org"); err != nil {
		result.Err = &ToolError{ErrorCode: "invalid_tool", Message: err.Error()}
		result.Duration = time.Since(start)
		return result
	}
	org := args["org"]
	workspace := strings.TrimSpace(args["workspace"])

	graph, scanned, skipped, gErr := buildOrgDependencyGraph(ctx, org, timeoutSec)
	if gErr != nil {
		result.Err = gErr
		result.Duration = time.Since(start)
		return result
	}

	if workspace != "" {
		payload := buildSingleWorkspaceView(workspace, org, graph)
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

	payload := buildOrgWideView(org, graph, scanned, skipped)
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

// remoteStateRef is one terraform_remote_state data source pointing at another
// workspace, with the outputs that the consumer reads from it.
type remoteStateRef struct {
	TargetOrg       string   `json:"target_org,omitempty"`
	TargetWorkspace string   `json:"target_workspace"`
	OutputsConsumed []string `json:"outputs_consumed,omitempty"`
}

// dependencyGraph maps workspace name → list of remote-state references
// originating in that workspace.
type dependencyGraph map[string][]remoteStateRef

// buildOrgDependencyGraph fans out across every workspace in the org,
// downloads each state, and parses terraform_remote_state references. Returns
// (graph, scanned, skipped, error). A per-workspace download failure is not
// fatal — those workspaces are counted as skipped.
func buildOrgDependencyGraph(ctx context.Context, org string, timeoutSec int) (dependencyGraph, int, int, *ToolError) {
	wsNames, ferr := listOrgWorkspaceNames(ctx, org, timeoutSec)
	if ferr != nil {
		return nil, 0, 0, ferr
	}

	graph := dependencyGraph{}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		skipped int
	)
	sem := make(chan struct{}, 4)

	for _, name := range wsNames {
		wg.Add(1)
		go func(ws string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			raw, dErr := downloadStateJSON(ctx, org, ws, timeoutSec)
			if dErr != nil {
				mu.Lock()
				skipped++
				graph[ws] = nil
				mu.Unlock()
				return
			}
			refs := parseRemoteStateRefs(raw)
			mu.Lock()
			graph[ws] = refs
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return graph, len(wsNames), skipped, nil
}

// listOrgWorkspaceNames pulls the list of workspace names in the org via
// `hcptf workspace list`. The CLI returns an array of objects with a `Name`
// (or `name`) field per entry.
func listOrgWorkspaceNames(ctx context.Context, org string, timeoutSec int) ([]string, *ToolError) {
	raw, ferr := fetchHCPTFJSON(ctx, timeoutSec, "workspace", "list", "-org="+org, "-output=json")
	if ferr != nil {
		return nil, ferr
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, &ToolError{ErrorCode: "parse_error", Message: "decode workspace list: " + err.Error()}
	}
	out := make([]string, 0, len(arr))
	for _, m := range arr {
		if name := firstStringField(m, "Name", "name"); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// downloadStateJSON shells out to `hcptf state download` to fetch the current
// state JSON for a workspace. Returns the raw state bytes (standard Terraform
// state format) or a normalized ToolError.
func downloadStateJSON(ctx context.Context, org, workspace string, timeoutSec int) ([]byte, *ToolError) {
	return fetchHCPTFJSON(ctx, timeoutSec, "state", "download", "-org="+org, "-workspace="+workspace)
}

// parseRemoteStateRefs walks a Terraform state JSON document looking for
// `data.terraform_remote_state` entries. For each one it extracts the
// referenced organization, workspace name, and the set of outputs the
// consumer reads. Workspace.prefix-style configs are intentionally not
// resolved (they expand to multiple workspaces at plan time and the state
// only records the fully-resolved instance, which we already capture via
// `workspaces.name` when present).
func parseRemoteStateRefs(stateJSON []byte) []remoteStateRef {
	if len(stateJSON) == 0 {
		return nil
	}
	var doc struct {
		Resources []struct {
			Mode      string `json:"mode"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Instances []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(stateJSON, &doc); err != nil {
		return nil
	}

	out := []remoteStateRef{}
	for _, r := range doc.Resources {
		if r.Mode != "data" || r.Type != "terraform_remote_state" {
			continue
		}
		for _, inst := range r.Instances {
			ref := remoteStateRefFromInstance(inst.Attributes)
			if ref.TargetWorkspace == "" {
				continue
			}
			out = append(out, ref)
		}
	}
	return dedupeRefs(out)
}

func remoteStateRefFromInstance(attrs map[string]any) remoteStateRef {
	if attrs == nil {
		return remoteStateRef{}
	}
	cfg, _ := attrs["config"].(map[string]any)
	var (
		targetOrg   string
		targetWS    string
		outputsRead []string
	)
	if cfg != nil {
		targetOrg = firstStringField(cfg, "organization")
		if wsBlock, ok := cfg["workspaces"].(map[string]any); ok {
			targetWS = firstStringField(wsBlock, "name")
		}
	}
	if outs, ok := attrs["outputs"].(map[string]any); ok {
		for k := range outs {
			outputsRead = append(outputsRead, k)
		}
		sort.Strings(outputsRead)
	}
	return remoteStateRef{
		TargetOrg:       targetOrg,
		TargetWorkspace: targetWS,
		OutputsConsumed: outputsRead,
	}
}

func dedupeRefs(refs []remoteStateRef) []remoteStateRef {
	seen := map[string]bool{}
	out := make([]remoteStateRef, 0, len(refs))
	for _, r := range refs {
		key := r.TargetOrg + "/" + r.TargetWorkspace
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// buildSingleWorkspaceView produces the per-workspace payload (depends_on,
// depended_by, depth, root/leaf flags) from the org-wide graph.
func buildSingleWorkspaceView(workspace, org string, graph dependencyGraph) map[string]any {
	dependsOn := []map[string]any{}
	for _, r := range graph[workspace] {
		entry := map[string]any{
			"workspace": r.TargetWorkspace,
			"evidence":  fmt.Sprintf("data.terraform_remote_state references workspace %s", r.TargetWorkspace),
		}
		if len(r.OutputsConsumed) > 0 {
			entry["outputs_consumed"] = r.OutputsConsumed
		}
		dependsOn = append(dependsOn, entry)
	}

	dependedBy := []map[string]any{}
	for ws, refs := range graph {
		if ws == workspace {
			continue
		}
		for _, r := range refs {
			if r.TargetWorkspace == workspace {
				dependedBy = append(dependedBy, map[string]any{
					"workspace": ws,
					"evidence":  fmt.Sprintf("%s state references this workspace's outputs", ws),
				})
				break
			}
		}
	}
	sort.Slice(dependedBy, func(i, j int) bool {
		return dependedBy[i]["workspace"].(string) < dependedBy[j]["workspace"].(string)
	})

	upstream := edgesUpstream(graph)
	depth := computeUpstreamDepth(upstream, workspace)
	isRoot := len(dependsOn) == 0
	isLeaf := len(dependedBy) == 0

	payload := map[string]any{
		"workspace":         workspace,
		"org":               org,
		"depends_on":        dependsOn,
		"depended_by":       dependedBy,
		"dependency_depth":  depth,
		"is_root":           isRoot,
		"is_leaf":           isLeaf,
	}
	if len(dependsOn) == 0 && len(dependedBy) == 0 {
		payload["note"] = "No cross-workspace dependencies detected. This workspace appears to be self-contained."
	} else {
		payload["note"] = "Dependency detection is based on terraform_remote_state data sources in workspace state files. Direct module dependencies within a workspace are not detected."
	}
	return payload
}

// buildOrgWideView produces the org-wide graph payload.
func buildOrgWideView(org string, graph dependencyGraph, scanned, skipped int) map[string]any {
	type node struct {
		Workspace  string   `json:"workspace"`
		DependsOn  []string `json:"depends_on"`
		DependedBy []string `json:"depended_by"`
		IsRoot     bool     `json:"is_root"`
		IsLeaf     bool     `json:"is_leaf"`
	}

	dependedByMap := map[string][]string{}
	for ws, refs := range graph {
		for _, r := range refs {
			dependedByMap[r.TargetWorkspace] = append(dependedByMap[r.TargetWorkspace], ws)
		}
	}

	names := make([]string, 0, len(graph))
	for ws := range graph {
		names = append(names, ws)
	}
	sort.Strings(names)

	nodes := make([]node, 0, len(names))
	roots := []string{}
	leaves := []string{}
	totalEdges := 0

	for _, ws := range names {
		refs := graph[ws]
		dependsOn := make([]string, 0, len(refs))
		for _, r := range refs {
			dependsOn = append(dependsOn, r.TargetWorkspace)
		}
		sort.Strings(dependsOn)

		dependedBy := append([]string(nil), dependedByMap[ws]...)
		sort.Strings(dependedBy)

		n := node{
			Workspace:  ws,
			DependsOn:  dependsOn,
			DependedBy: dependedBy,
			IsRoot:     len(dependsOn) == 0,
			IsLeaf:     len(dependedBy) == 0,
		}
		nodes = append(nodes, n)
		totalEdges += len(dependsOn)
		if n.IsRoot {
			roots = append(roots, ws)
		}
		if n.IsLeaf {
			leaves = append(leaves, ws)
		}
	}

	note := "Dependency detection is based on terraform_remote_state data sources in workspace state files."
	if totalEdges == 0 {
		note = "No cross-workspace terraform_remote_state references detected anywhere in this organization. Workspaces appear to be self-contained."
	}

	return map[string]any{
		"org":                       org,
		"total_workspaces_scanned":  scanned,
		"workspaces_skipped":        skipped,
		"dependency_graph":          nodes,
		"roots":                     roots,
		"leaves":                    leaves,
		"total_dependency_edges":    totalEdges,
		"note":                      note,
	}
}

// edgesUpstream inverts the graph: result[ws] is the list of workspaces that
// depend on ws (i.e., direct upstream consumers).
func edgesUpstream(graph dependencyGraph) map[string][]string {
	out := map[string][]string{}
	for ws, refs := range graph {
		for _, r := range refs {
			out[r.TargetWorkspace] = append(out[r.TargetWorkspace], ws)
		}
	}
	return out
}

// computeUpstreamDepth returns the longest chain length downstream of `start`
// in the upstream graph — i.e., how many transitive consumers exist before
// reaching a leaf. BFS is sufficient because the graph is a DAG in practice
// (Terraform doesn't allow cyclic remote_state references); a `visited` guard
// keeps any malformed cycle from looping forever.
func computeUpstreamDepth(upstream map[string][]string, start string) int {
	if len(upstream[start]) == 0 {
		return 0
	}
	visited := map[string]int{start: 0}
	queue := []string{start}
	maxDepth := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		d := visited[node]
		for _, next := range upstream[node] {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = d + 1
			if d+1 > maxDepth {
				maxDepth = d + 1
			}
			queue = append(queue, next)
		}
	}
	return maxDepth
}
