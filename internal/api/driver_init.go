package api

import (
	"github.com/heavenlabs/hnb/internal/executor"
)

func init() {
	executor.RegisterDriver(&executor.PostgresDriver{})
	executor.RegisterDriver(&executor.ClickHouseDriver{})
	executor.RegisterDriver(&executor.OpenSearchDriver{})
}
