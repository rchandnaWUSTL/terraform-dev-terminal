# Terraform Dev — Agent Instructions

You are building **Terraform Dev**: an AI-native terminal REPL where infrastructure engineers describe intent in plain English and an agent drives HCP Terraform end-to-end.

## Before writing any code

1. Read `prd.md` in full
2. Create an aiki task for the overall project, then break it into subtasks with dependencies
3. Confirm your proposed tech stack and architecture with me before proceeding

## Task tracking

Use `aiki task` for all task tracking. No TodoWrite, no mental checklists, no bd.

- `aiki task start "Description"` before any file modification
- `aiki task comment add <id> "progress note"` during long tasks
- `aiki task close <id> --confidence <1-4> --summary "what you did"` when done
- Use `--subtask-of <parent-id>` to express dependencies between layers

Suggested task structure:
- Parent: Terraform Dev v0.1
  - Subtask: Repo scaffolding and Go module setup
  - Subtask: Auth gate (hcptf credential check on startup)
  - Subtask: Tool layer (6 tools, hcptf shell-out, error normalization)
  - Subtask: Agent loop (planner + streaming summarizer, Anthropic API)
  - Subtask: Terminal REPL (prompt, rendering, slash commands)
  - Subtask: Demo script and smoke test

## Key constraints from the PRD

- `hcptf` CLI is a read-only dependency — do not modify it
- Read-only mode is the default; mutations require `--apply` flag
- Agent responses must stream token-by-token
- Auth check on startup delegates to existing `hcptf` credential chain — no custom auth logic
- Model pre-configured as Claude Sonnet 4.6 in `~/.terraform-dev/config.yaml`
- No MCP server, no local `.tf` file awareness — explicitly out of scope for v0.1
