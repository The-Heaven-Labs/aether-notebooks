package executor_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/executor"
)

func denoAvailable() bool {
	_, err := exec.LookPath("deno")
	return err == nil
}

func TestJSExecutorOutput(t *testing.T) {
	if !denoAvailable() {
		t.Skip("deno not installed")
	}

	js := executor.NewJSExecutor(10 * time.Second)
	out, err := js.Execute(context.Background(), `console.log("hello from deno")`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "hello from deno") {
		t.Fatalf("expected 'hello from deno' in output, got: %q", out)
	}
}

func TestJSExecutorTimeout(t *testing.T) {
	if !denoAvailable() {
		t.Skip("deno not installed")
	}

	js := executor.NewJSExecutor(200 * time.Millisecond)
	_, err := js.Execute(context.Background(), `while(true){}`, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
