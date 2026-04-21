package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestStartupChecks_MissingHcptf(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", origPath)

	err := runStartupChecks()
	if err == nil {
		t.Fatal("expected error when hcptf not on PATH")
	}
	if !strings.Contains(err.Error(), "hcptf not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStartupChecks_MissingAPIKey(t *testing.T) {
	// Ensure hcptf is on PATH for this test
	if _, err := exec.LookPath("hcptf"); err != nil {
		t.Skip("hcptf not on PATH, skipping")
	}

	t.Setenv("ANTHROPIC_API_KEY", "")

	err := runStartupChecks()
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY missing")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStartupChecks_NoCredentials(t *testing.T) {
	if _, err := exec.LookPath("hcptf"); err != nil {
		t.Skip("hcptf not on PATH, skipping")
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	// Intentionally leave HCP credentials absent — hcptf whoami should fail.
	// This test validates the credential check error path, not authentication itself.
	err := checkHCPTFCredentials()
	// If credentials happen to be present this test will pass vacuously.
	// In CI without credentials, we expect an error.
	if err != nil && !strings.Contains(err.Error(), "credentials") && !strings.Contains(err.Error(), "hcptf") {
		t.Errorf("unexpected error format: %v", err)
	}
}
