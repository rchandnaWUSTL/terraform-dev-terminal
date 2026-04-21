# Terraform Dev — Demo Script

## Setup

```bash
# 1. Install hcptf
curl -sLO https://github.com/thrashr888/hcptf-cli/releases/download/v0.6.0/hcptf-cli_0.6.0_darwin_arm64.tar.gz
tar -xzf hcptf-cli_0.6.0_darwin_arm64.tar.gz
mv hcptf ~/bin/hcptf   # or sudo mv hcptf /usr/local/bin/hcptf

# 2. Authenticate
hcptf login

# 3. Set API key
export ANTHROPIC_API_KEY=your-key

# 4. Build and run
make build
./terraform-dev --org=my-org --workspace=prod-us-east-1
```

## Demo Prompts (in order)

### 1. Topology description
```
hcp-tf> Describe the prod-us-east-1 workspace
```
Calls: `_hcp_tf_workspace_describe`

### 2. Drift report
```
hcp-tf> Any of my workspaces drifted this week?
```
Calls: `_hcp_tf_drift_detect`

### 3. Recent runs
```
hcp-tf> Show me the last 5 runs in prod-us-east-1 and flag anything concerning
```
Calls: `_hcp_tf_runs_list_recent`

### 4. Prod-to-staging safety check
```
hcp-tf> Is it safe to apply my latest prod changes to staging?
```
Calls: `_hcp_tf_runs_list_recent` → `_hcp_tf_workspace_diff` (prod) → `_hcp_tf_workspace_diff` (staging)

### 5. Policy check (requires a run ID from step 3)
```
hcp-tf> What were the policy results for run-abc123?
```
Calls: `_hcp_tf_policy_check`

### 6. Plan summary (requires a run ID)
```
hcp-tf> Summarize the plan for run-abc123 — what are the risks?
```
Calls: `_hcp_tf_plan_summary`

## Slash commands to demo

```
/org my-org          # switch org
/workspace staging   # switch default workspace
/mode                # confirm readonly mode
/reset               # clear history for a clean follow-up
/help                # show available commands
```

## Expected behavior

- Tool calls show: `⟳ _hcp_tf_runs_list_recent  org=my-org workspace=prod-us-east-1`
- On success: `✓ _hcp_tf_runs_list_recent (342ms)  [{"id":"run-...`
- AI response streams in white, line by line — no waiting for completion
- `/exit` or Ctrl-D exits cleanly
