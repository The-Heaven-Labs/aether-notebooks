package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type JSExecutor struct {
	timeout time.Duration
}

func NewJSExecutor(timeout time.Duration) *JSExecutor {
	return &JSExecutor{timeout: timeout}
}

func (j *JSExecutor) Databases(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (j *JSExecutor) Execute(ctx context.Context, source string, inputData interface{}) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	wrapper := fmt.Sprintf(`
const data = JSON.parse(await new Response(Deno.stdin.readable).text());
%s
`, source)

	inputJSON, _ := json.Marshal(inputData)

	cmd := exec.CommandContext(ctx, "deno", "eval", wrapper)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("js execution failed: %s: %w", stderr.String(), err)
	}

	return stdout.String(), nil
}
