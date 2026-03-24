package executor_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/executor"
)

func TestClickHouseExecutorInterface(t *testing.T) {
	// Verify ClickHouseExecutor implements Executor
	var _ executor.Executor = (*executor.ClickHouseExecutor)(nil)
}
