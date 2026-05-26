package env

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalEnvironment_InitSession(t *testing.T) {
	cwd, _ := os.Getwd()
	env := NewLocalEnvironment(cwd, 30*time.Second)
	defer env.Close()

	err := env.InitSession(context.Background())
	if err != nil {
		t.Fatalf("InitSession failed: %v", err)
	}

	// After init, CWD should match the current directory
	tracked := env.CWD()
	if tracked != cwd {
		// CWD might be resolved to a different path via symlink
		// Just check it's not empty
		if tracked == "" {
			t.Error("CWD is empty after init")
		}
	}
}

func TestLocalEnvironment_EnvPersistence(t *testing.T) {
	cwd, _ := os.Getwd()
	env := NewLocalEnvironment(cwd, 30*time.Second)
	defer env.Close()

	if err := env.InitSession(context.Background()); err != nil {
		t.Fatalf("InitSession failed: %v", err)
	}

	// Set a unique env var
	result, err := env.Execute(context.Background(), "export PUX_TEST_VAR=hello_pux", ExecuteOptions{})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Verify the env var persists in the next call
	result, err = env.Execute(context.Background(), "echo $PUX_TEST_VAR", ExecuteOptions{})
	if err != nil {
		t.Fatalf("echo failed: %v", err)
	}

	if !strings.Contains(result.Output, "hello_pux") {
		t.Errorf("env var not persisted: output = %q", result.Output)
	}
}

func TestLocalEnvironment_CWDTracking(t *testing.T) {
	cwd, _ := os.Getwd()
	env := NewLocalEnvironment(cwd, 30*time.Second)
	defer env.Close()

	if err := env.InitSession(context.Background()); err != nil {
		t.Fatalf("InitSession failed: %v", err)
	}

	// cd to /tmp
	_, err := env.Execute(context.Background(), "cd /tmp", ExecuteOptions{})
	if err != nil {
		t.Fatalf("cd failed: %v", err)
	}

	// Verify CWD tracked
	if env.CWD() != "/tmp" {
		t.Errorf("CWD = %q, want /tmp", env.CWD())
	}

	// Verify next command runs in /tmp
	result, err := env.Execute(context.Background(), "pwd", ExecuteOptions{})
	if err != nil {
		t.Fatalf("pwd failed: %v", err)
	}

	if !strings.Contains(result.Output, "/tmp") {
		t.Errorf("pwd output = %q, want /tmp", result.Output)
	}
}

func TestLocalEnvironment_SecurityGuard(t *testing.T) {
	cwd, _ := os.Getwd()
	env := NewLocalEnvironment(cwd, 30*time.Second)
	defer env.Close()

	if err := env.InitSession(context.Background()); err != nil {
		t.Fatalf("InitSession failed: %v", err)
	}

	// Attempt to write to .ssh/authorized_keys — should be blocked
	result, err := env.Execute(context.Background(),
		"echo 'ssh-rsa ...' > ~/.ssh/authorized_keys",
		ExecuteOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != 126 {
		t.Errorf("exit code = %d, want 126 (security block)", result.ExitCode)
	}
	if !strings.Contains(result.Output, "denied") {
		t.Errorf("output should mention denial: %q", result.Output)
	}
}

func TestLocalEnvironment_ExitCode(t *testing.T) {
	cwd, _ := os.Getwd()
	env := NewLocalEnvironment(cwd, 30*time.Second)
	defer env.Close()

	if err := env.InitSession(context.Background()); err != nil {
		t.Fatalf("InitSession failed: %v", err)
	}

	// Successful command
	result, err := env.Execute(context.Background(), "true", ExecuteOptions{})
	if err != nil {
		t.Fatalf("true failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}

	// Failing command — Environment returns non-zero in ExecuteResult, not as error
	result, err = env.Execute(context.Background(), "false", ExecuteOptions{})
	if err != nil {
		t.Fatalf("false should not return error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("exit code = 0, want non-zero for 'false'")
	}
}

func TestAsExecutor_Adapter(t *testing.T) {
	cwd, _ := os.Getwd()
	env := NewLocalEnvironment(cwd, 30*time.Second)
	defer env.Close()

	if err := env.InitSession(context.Background()); err != nil {
		t.Fatalf("InitSession failed: %v", err)
	}

	// Wrap as bash.Executor
	exec := AsExecutor(env)

	// Test via bash.Executor interface
	output, err := exec.Exec(context.Background(), "echo hello_from_adapter")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "hello_from_adapter") {
		t.Errorf("output = %q, want to contain hello_from_adapter", output)
	}

	// Test that non-zero exit returns error
	_, err = exec.Exec(context.Background(), "false")
	if err == nil {
		t.Error("expected error for non-zero exit code")
	}
}
