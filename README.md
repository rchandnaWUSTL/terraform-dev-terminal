# Terraform Dev

AI-native terminal REPL for HCP Terraform.

![Demo](demo-v2.gif)

## Quick start

Prerequisites:

- Go 1.23+
- [`hcptf`](https://github.com/thrashr888/hcptf-cli/releases) on your `PATH`
- `ANTHROPIC_API_KEY` set in your environment

Install:

```bash
git clone https://github.com/rchandnaWUSTL/terraform-dev-terminal.git
cd terraform-dev-terminal
go build -o terraform-dev ./cmd/terraform-dev
./terraform-dev --org=<your-org> --workspace=<your-workspace>
```

---

Built on [hcptf](https://github.com/thrashr888/hcptf-cli) by [@thrashr888](https://github.com/thrashr888).
