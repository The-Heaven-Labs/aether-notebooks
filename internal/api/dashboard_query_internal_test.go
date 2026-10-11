package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// dashboardQueryTestParams is the zero-infrastructure parameter set shared by
// the single-flight tests: the public cache scope resolves the access
// fingerprint without a DB lookup, and a zero-value Server skips the Redis
// get/set (Cache == nil).
func dashboardQueryTestParams() dashboardQueryParams {
	return dashboardQueryParams{
		OrgID:       "o",
		Identity:    dashboardIdentity{UserID: "u"},
		ConnectorID: "c",
		SQL:         "SELECT 1",
		CacheScope:  "token:t",
	}
}

// TestDashboardQuerySingleFlight pins per-cache-key dedupe: concurrent
// identical misses run the query exactly once, and every follower receives the
// leader's result instead of computing its own. The compute override replaces
// the real execution so the test needs neither a database nor Redis.
func TestDashboardQuerySingleFlight(t *testing.T) {
	s := &Server{}
	var calls atomic.Int32
	s.dashboardQueryCompute = func(ctx context.Context, p dashboardQueryParams) (*dashboardQueryResponse, error) {
		calls.Add(1)
		time.Sleep(200 * time.Millisecond)
		return dashboardQueryResult(&executor.ResultSet{Columns: nil, Rows: [][]any{{1}}}, 5, false, nil), nil
	}

	p := dashboardQueryTestParams()
	const goroutines = 8
	var wg sync.WaitGroup
	responses := make([]*dashboardQueryResponse, goroutines)
	errs := make([]error, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			responses[i], errs[i] = s.runDashboardQuery(context.Background(), p)
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, int32(1), calls.Load(), "concurrent identical misses must run one query")
	for i := 0; i < goroutines; i++ {
		require.NoError(t, errs[i])
		require.NotNil(t, responses[i])
		require.Same(t, responses[0], responses[i], "followers must share the leader's result")
	}
	// The shared response carries the override's marker row, so followers
	// received the leader's computation rather than executing their own.
	rs, ok := responses[0].Outputs[0].Data.(*executor.ResultSet)
	require.True(t, ok)
	require.Equal(t, [][]any{{1}}, rs.Rows)
}

// TestDashboardQueryComputeDetachedFromCallerCancellation pins the flight's
// deliberate context detachment: one caller aborting must not cancel the
// shared computation (followers would see a spurious 422). A canceled caller
// still receives the completed result; the connector/default timeout is the
// bound.
func TestDashboardQueryComputeDetachedFromCallerCancellation(t *testing.T) {
	s := &Server{}
	s.dashboardQueryCompute = func(ctx context.Context, p dashboardQueryParams) (*dashboardQueryResponse, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return dashboardQueryResult(&executor.ResultSet{Columns: nil, Rows: [][]any{{1}}}, 5, false, nil), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := s.runDashboardQuery(ctx, dashboardQueryTestParams())
	require.NoError(t, err, "the shared computation must not inherit the caller's cancellation")
	require.NotNil(t, resp)
}
