package executor

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/models"
)

// TestClickHouseConnectBudgetCoversIdleCloudWake guards the connection budget
// for the first query after a ClickHouse Cloud service idles: clickhouse-go
// applies DialTimeout to the native handshake, and its 30s default is shorter
// than a service wake can take.
func TestClickHouseConnectBudgetCoversIdleCloudWake(t *testing.T) {
	require.GreaterOrEqual(t, clickHouseConnectTimeout, time.Minute,
		"idle ClickHouse Cloud wake can exceed the driver's 30s default DialTimeout")
}

// TestClickHouseHandshakeUsesConnectBudget proves the configured budget reaches
// the native handshake. A silent TCP peer completes the TCP connection but
// never answers the handshake; the attempt must expire at the configured
// connect budget rather than the driver's 30s default (or the old 10s ping
// deadline).
func TestClickHouseHandshakeUsesConnectBudget(t *testing.T) {
	previous := clickHouseConnectTimeout
	clickHouseConnectTimeout = 250 * time.Millisecond
	t.Cleanup(func() { clickHouseConnectTimeout = previous })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()

	start := time.Now()
	_, err = NewClickHouseExecutor(models.ConnectorConfig{
		Host:     "127.0.0.1",
		Port:     listener.Addr().(*net.TCPAddr).Port,
		User:     "u",
		Password: "p",
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "handshake")
	require.GreaterOrEqual(t, elapsed, 200*time.Millisecond)
	require.Less(t, elapsed, 5*time.Second,
		"handshake should use the configured connect budget, not the driver default")
}
