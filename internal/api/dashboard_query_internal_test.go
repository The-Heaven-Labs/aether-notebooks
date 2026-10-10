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

// TestDashboardQuerySingleFlight pins per-cache-key dedupe: concurrent
// identical misses run the query exactly once, and every follower receives the
// leader's result instead of computing its own. The compute hook replaces the
// real execution so the test needs neither a database nor Redis; the public
// cache scope resolves the access fingerprint without a DB lookup, and the
// zero-value Server skips the Redis get/set (Cache == nil).
func TestDashboardQuerySingleFlight(t *testing.T) {
	s := &Server{}
	var calls atomic.Int32
	old := dashboardQueryComputeHook
	dashboardQueryComputeHook = func(srv *Server, ctx context.Context, p dashboardQueryParams) (*dashboardQueryResponse, error) {
		calls.Add(1)
		time.Sleep(200 * time.Millisecond)
		return dashboardQueryResult(&executor.ResultSet{Columns: nil, Rows: [][]any{{1}}}, 5, false, nil), nil
	}
	t.Cleanup(func() { dashboardQueryComputeHook = old })

	p := dashboardQueryParams{
		OrgID:       "o",
		Identity:    dashboardIdentity{UserID: "u"},
		ConnectorID: "c",
		SQL:         "SELECT 1",
		CacheScope:  "token:t",
	}
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
	// The shared response carries the hook's marker row, so followers received
	// the leader's computation rather than executing their own.
	rs, ok := responses[0].Outputs[0].Data.(*executor.ResultSet)
	require.True(t, ok)
	require.Equal(t, [][]any{{1}}, rs.Rows)
}
