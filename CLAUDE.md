# Terraform Dev — Agent Instructions

You are working on **Terraform Dev**: an AI-native terminal REPL where infrastructure engineers describe intent in plain English and an agent drives HCP Terraform end-to-end.

v0.1–v0.6 are shipped. The codebase runs against a live HCP Terraform org.

## Before writing any code

1. Read `prd.md` for product context and the current tool surface
2. Read `ROADMAP.md` to understand what is shipped vs. what is queued
3. Start an aiki task before touching files: `aiki task start "..."`; close it with a concise summary when done

## Task tracking

Use `aiki task` for all task tracking. No TodoWrite, no mental checklists.

- `aiki task start "Description"` before any file modification
- `aiki task comment add <id> "progress note"` during long-running work
- `aiki task close <id> --summary "what you did"` when done (or `--wont-do` if abandoning)
- Use `--subtask-of <parent-id>` for dependency chains

If an `aiki build` autonomous run is dispatched and a subtask stalls for more than ~15 minutes, close the stalled subtask with `aiki task close <id> --wont-do --summary "Stalled — manual recovery"` and finish the remaining work manually. A stalled orchestrator should never block an epic.

## Repository layout

- `cmd/terraform-dev/` — entrypoint, flag parsing, startup auth check
- `internal/config/` — YAML config loader (`~/.terraform-dev/config.yaml`)
- `internal/provider/` — `ModelProvider` interface + Anthropic, OpenAI, Copilot implementations
- `internal/providerfactory/` — wires provider selection from auth mode + config
- `internal/tools/` — tool dispatch, hcptf shell-out, error normalization, audit log
- `internal/agent/` — planner + summarizer loop, system prompt, approval callback plumbing
- `internal/repl/` — readline loop, streaming renderer, approval gate, code-block extraction
- `ops/now/` — ephemeral specs handed to `aiki build` for autonomous execution

## Build and test

```bash
go build -o terraform-dev ./cmd/terraform-dev
go test ./...
```

## Run the REPL

```bash
./terraform-dev --org=<org> --workspace=<ws> --auth=copilot            # readonly
./terraform-dev --org=<org> --workspace=<ws> --auth=copilot --apply    # mutation-enabled
```

Test org: `sarah-test-org`, workspace: `prod-k8s-apps` (2 null_resources, safe to mutate).
Copilot token cached at `~/.terraform-dev/copilot.json`; `ANTHROPIC_API_KEY` is a fallback.

## Key constraints

- `hcptf` CLI is a read-only dependency — do not modify it
- Read-only is the default; mutations require the `--apply` flag at startup
- Mutating tools are filtered out of the tool definitions in readonly mode — the model never sees them
- Every mutation flows through a synchronous REPL approval gate before `tools.Call` runs
- Plans with destroys > 0 require a second `yes` at the apply gate
- If the user cancels an apply after a run was created, the REPL auto-invokes `_hcp_tf_run_discard`
- Agent responses stream token-by-token
- Every tool call — including gate cancellations — is appended as a JSON line to `~/.terraform-dev/audit.log`
- The system prompt is mode-aware (readonly vs. apply) and always carries the config-generation rules
- Generated HCL in fenced `hcl`/`terraform`/`tf` blocks is written to the cwd by the REPL; existing files prompt before overwrite; validation runs automatically after write

## Conventions

- Never surface run IDs, plan IDs, or workspace IDs in agent prose — human names only
- Tool error shape is always `{ error_code, message, retryable }`
- HTML responses from HCP Terraform (plan-tier gates) are normalized to a 404 tool error
- HashiCorp brand colors: `tfPurple` (tool calls), `waypointTeal` (success), `vaultYellow` (warnings/approvals), `boundaryPink` (errors/cancellations)
- Commits: `feat:` / `fix:` / `docs:` prefix, imperative subject, short body paragraph, trailing `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>` when Claude Code authored the change
