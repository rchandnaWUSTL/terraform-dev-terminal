package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/rchandnaWUSTL/terraform-dev/internal/config"
	"github.com/rchandnaWUSTL/terraform-dev/internal/repl"
)

var (
	bold = color.New(color.Bold)
	red  = color.New(color.FgRed)
)

func main() {
	org := flag.String("org", "", "HCP Terraform organization")
	workspace := flag.String("workspace", "", "HCP Terraform workspace")
	flag.Parse()

	if err := runStartupChecks(); err != nil {
		fmt.Fprintln(os.Stderr)
		red.Fprintln(os.Stderr, "  "+err.Error())
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		red.Fprintf(os.Stderr, "  ✗ Failed to load config: %v\n", err)
		os.Exit(1)
	}

	r := repl.New(cfg, *org, *workspace)
	if err := r.Run(); err != nil {
		red.Fprintf(os.Stderr, "  ✗ %v\n", err)
		os.Exit(1)
	}
}

func runStartupChecks() error {
	if _, err := exec.LookPath("hcptf"); err != nil {
		return fmt.Errorf("✗ hcptf not found. Install it and ensure it's on your PATH.\n    https://github.com/thrashr888/hcptf-cli/releases")
	}

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("✗ ANTHROPIC_API_KEY not found in environment.\n    export ANTHROPIC_API_KEY=your-key")
	}

	if err := checkHCPTFCredentials(); err != nil {
		return err
	}

	return nil
}

func checkHCPTFCredentials() error {
	cmd := exec.Command("hcptf", "whoami", "-output=json")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		msg := "✗ No HCP Terraform credentials found.\n    Run: hcptf login\n    Then try: terraform dev"
		if e, ok := err.(*exec.ExitError); ok {
			exitErr = e
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" && !strings.Contains(strings.ToLower(stderr), "credentials") &&
				!strings.Contains(strings.ToLower(stderr), "unauthorized") {
				msg = fmt.Sprintf("✗ hcptf whoami failed: %s\n    Run: hcptf login", stderr)
			}
		}
		return fmt.Errorf("%s", msg)
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return fmt.Errorf("✗ hcptf returned unexpected output. Run: hcptf login")
	}

	return nil
}
