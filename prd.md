# Terraform Dev — PRD v0.1

**Status:** Draft  
**Owner:** Roshan Chandna  
**Team:** PAVE / AGX

---

## 1. Overview

Terraform Dev is an AI-native terminal REPL that lets infrastructure engineers describe what they want in plain English and have an agent drive HCP Terraform end-to-end: reading configs and state, proposing changes, running plans, interpreting diffs, enforcing policy, applying with guardrails, and reporting back.

You launch it with a single command:

```
$ terraform dev
```

From there, the prompt replaces raw CLI incantations:

```
hcp-tf> Show me my latest prod run and tell me if it's safe to apply similar changes to staging.
```

The agent calls the right tools, reasons over structured output, and streams a response in plain English with specific next steps — then waits for your approval before doing anything destructive.

---

## 2. Problem

Terraform practitioners today face a multi-tool coordination problem. A typical infra change requires:

- Running `terraform plan` and manually inspecting hundreds of lines of diff
- Cross-referencing state files, variable files, and the cloud console to understand actual topology
- Separately checking Sentinel/OPA policy results and cost estimates
- Deciding when to apply based on experience and intuition, not structured analysis
- Doing all of this across multiple workspaces and environments manually

Claude Code can edit `.tf` files and run commands. But it lacks the opinionated tool surface that encodes HCP Terraform concepts: workspaces, runs, drift, cost, policies, identity, and audit. Engineers still manually orchestrate the loop.

The gap is not "LLM plus CLI." The gap is a coherent agent loop that already understands the HCP Terraform data model, respects org identity and policy, and surfaces the right information at the right time.

The differentiated asset is not the agent runtime. It is the tooling contract: agent-first CLI commands that already encode HCP Terraform concepts, respect org identity, produce structured output, and emit audit logs. Terraform Dev is the thin, opinionated shell on top.

---

## 3. Goals

### In scope (v0.1 — demoable)

- Working REPL with `terraform dev` entrypoint, `hcp-tf>` prompt, history, and `/help`
- Agent loop: natural language → tool planning → structured tool calls → streamed narrative synthesis
- Read-only mode by default; explicit `--apply` flag required for mutations
- 6 core tool implementations backed by the `hcptf` CLI
- Terminal rendering: tool call labels, truncated JSON blobs, color-coded streamed AI response
- Workspace and org context pinning via flags (`--workspace`, `--org`)
- Auth gate on startup: check for existing `hcptf` credentials, exit cleanly if missing
- Model pre-configured as Claude Sonnet 4.6 via `~/.terraform-dev/config.yaml`

### Out of scope (v0.1)

- Local `.tf` file awareness (deferred — opens significant UX complexity around repo/workspace binding)
- MCP server wrapper (no external consumers yet; build when there is one)
- CI/CD integration, PR creation
- Multi-step approval workflows with structured sign-off
- Cost estimation integration
- Custom policy override flows
- User-configurable model selection in the terminal

---

## 4. User Stories

Primary persona: Platform Engineer or Senior DevOps Engineer who owns HCP Terraform at their org.

| As a... | I want to... |
|---|---|
| Platform engineer | Ask in plain English whether it's safe to apply prod changes to staging, and get a structured answer without running 5 CLI commands. |
| DevOps lead | Describe a topology change in English, see the plan summarized as human costs and risks, and approve it — all without writing Terraform. |
| On-call SRE | Ask why an instance is unreachable and get a root-cause analysis pointing at SGs, routes, or IAM — not a list of commands to run myself. |

---

## 5. Architecture

Terraform Dev has four layers. Each has a clearly scoped responsibility and a clean interface to the next.

### Layer 1: Agent-ready CLI (hcptf)

The foundation is the `hcptf` CLI — a Go binary with 231+ commands across 60+ HCP Terraform resource types. What makes it agent-ready:

- All commands take explicit flags, no interactive prompts
- `-output=json` on every command returns machine-parsable structured data
- `-dry-run` on all mutations: validate without side effects
- `schema` introspection: `hcptf schema <command>` returns flag definitions as JSON
- URL-style read paths (`hcptf my-org my-workspace runs`) separate from flag-style write paths
- Consistent exit codes: 0 success, 1 API error, 2 usage error

The CLI is not modified for v0.1. It is treated as a stable, read-only dependency.

### Layer 2: Tool layer

A thin Go wrapper turns CLI commands into named tools the agent can call. Each tool:

- Has a clear name like `_hcp_tf_runs_list_recent` or `_hcp_tf_workspace_diff`
- Shells out to `hcptf` with the appropriate flags and `-output=json`
- Enforces a timeout (default 10s)
- Normalizes errors into `{ error_code, message, retryable }` — never passes raw stderr to the model
- Returns only structured JSON — no ANSI color codes, no interactive prompts

This is the surface exposed to the agent. It defines the contract between the LLM and the infrastructure.

### Layer 3: Agent loop

A minimal planner/summarizer loop built on the Anthropic API (Claude Sonnet 4.6):

- **Planner:** takes the user's natural language request, selects a short ordered toolchain (max 4 tools in v0.1), calls them in sequence
- **Summarizer:** takes the structured tool outputs and streams a narrative response with risks, costs, and next steps
- **No-apply mode:** the planner prompt explicitly forbids triggering mutations in v0.1
- **Streaming:** the agent response streams token-by-token for a terminal-native feel
- **Pluggable:** future guardrail steps (policy check, approval gate, blast radius check) slot in between planner and summarizer

### Layer 4: Terminal UX (`terraform dev`)

A long-running REPL process that renders the conversation:

- `hcp-tf>` prompt with readline history and basic editing
- `/help`, `/mode`, `/workspace`, `/org` slash commands
- Tool call rendering: label + flags on left, truncated JSON on right, green checkmark or red X
- AI response streams in a distinct color (white on dark background)
- `--readonly` flag (default on), `--workspace` and `--org` for context pinning
- ASCII art banner on startup, version and model info in status line

---

## 6. Tool Surface (v0.1)

Six tools for the initial demo. All are read-only. Each maps to one or more `hcptf` commands.

| Tool | What it does | hcptf command(s) |
|---|---|---|
| `_hcp_tf_runs_list_recent` | Lists the N most recent runs for a workspace with status, timestamps, resource counts, and cost delta | `hcptf <org> <workspace> runs -output=json` |
| `_hcp_tf_workspace_diff` | Compares two workspaces: missing resources, config drift, variable mismatches | `hcptf <org> <ws-a> state -output=json` + `hcptf <org> <ws-b> state -output=json`, diffed |
| `_hcp_tf_workspace_describe` | Returns topology narrative: resource types, providers, last run status, drift indicator | `hcptf <org> <workspace>` + `assessments` + `state outputs` |
| `_hcp_tf_drift_detect` | Returns workspaces with detected drift and the changed resources | `hcptf <org> <workspace> assessments -output=json` |
| `_hcp_tf_policy_check` | Returns policy check results for a run: which checks passed/failed, which rules fired | `hcptf <org> <workspace> runs <run-id> policychecks -output=json` |
| `_hcp_tf_plan_summary` | Returns a human-readable summary of a plan: adds/changes/destroys, flagged risks | `hcptf <org> <workspace> runs <run-id> plan -output=json` |

---

## 7. Example Interactions

### Prod-to-staging safety check

```
hcp-tf> Is it safe to apply my latest prod changes to staging?
```

Agent calls: `_hcp_tf_runs_list_recent` (prod) → `_hcp_tf_workspace_diff` (prod..staging) → `_hcp_tf_policy_check`

Response: "Your last prod run added 3 resources and changed 7 with no deletions (+$124/mo). Staging is missing WAF and ALB access logs, and has 0-day DB backup retention. I'd align staging's security posture first before mirroring this change. Want me to open a plan in staging with those gaps fixed?"

### Topology description

```
hcp-tf> Describe the prod-us-east-1 workspace
```

Agent calls: `_hcp_tf_workspace_describe`

Response: "prod-us-east-1 runs Terraform 1.7.4 with the AWS provider. It manages 47 resources across EC2, RDS, ALB, and WAF. Last run was 6 days ago (planned_and_finished). No drift detected. Monthly cost estimate: $2,340."

### Drift report

```
hcp-tf> Any of my workspaces drifted this week?
```

Agent calls: `_hcp_tf_drift_detect`

Response: "3 workspaces show drift: staging-us-east-1 (security group rule removed), dev-eu-west-1 (S3 bucket ACL changed), and data-us-east-1 (RDS parameter group modified). Want me to show the full diff for any of these?"

---

## 8. Governance Model

Every action runs under the user's scoped HCP Terraform identity (via the existing `hcptf` credential chain).

- **Read-only by default:** no mutations, no plans triggered, no applies. The agent can describe, compare, and summarize.
- **Auth gate on startup:** check for valid `hcptf` credentials on launch. If missing, print a single clear message and exit cleanly — no prompts inside the REPL:
  ```
  ✗ No HCP Terraform credentials found.
    Run: hcptf login
    Then try: terraform dev
  ```
- **Dry-run gate:** any proposed mutation is previewed with `-dry-run` before the user confirms
- **Structured approval:** the agent states exactly what it intends to do and waits for an explicit yes before proceeding
- **Audit trail:** all tool calls and their outputs are logged to `~/.terraform-dev/audit.log` with timestamps and user identity

---

## 9. Configuration

Config file at `~/.terraform-dev/config.yaml`. Created automatically on first run with defaults.

```yaml
model: claude-sonnet-4-6        # not user-configurable in v0.1 UI, but editable here
max_tokens: 16384
timeout_seconds: 10
readonly: true
```

Model is pre-configured. No in-terminal switcher in v0.1.

---

## 10. Success Metrics (Demo)

- End-to-end demo runs cleanly against a live HCP Terraform org with real workspaces
- Agent correctly selects the right tool sequence for at least 4 of the 6 canonical prompts
- Tool calls complete in under 5 seconds for all read operations
- Zero hallucinated resource names or run IDs in agent responses
- AI response streams token-by-token — no waiting for full completion before output appears
- Rendering matches the visual spec: color-coded, truncated JSON, distinct streamed AI response block
