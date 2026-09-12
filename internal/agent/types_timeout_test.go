package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testTool(name string, timeout time.Duration, h ToolHandler) *ToolDef {
	d := &ToolDef{Timeout: timeout, Handler: h}
	d.Function.Name = name
	return d
}

func TestToolDefExecuteAppliesTimeout(t *testing.T) {
	d := testTool("hang", 50*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
		<-tc.Context.Done()
		return nil, tc.Context.Err()
	})
	_, err := d.Execute(nil, &ToolContext{Context: context.Background()})
	require.Error(t, err)
	require.Contains(t, err.Error(), `tool "hang" timed out after 50ms`)
}

func TestToolDefExecuteNoTimeout(t *testing.T) {
	d := testTool("interactive", NoTimeout, func(_ json.RawMessage, tc *ToolContext) (any, error) {
		_, hasDeadline := tc.Context.Deadline()
		require.False(t, hasDeadline)
		return "ok", nil
	})
	d.TimeoutFromArgs = func(json.RawMessage) (time.Duration, bool) {
		return 10 * time.Millisecond, true
	}
	out, err := d.Execute(nil, &ToolContext{Context: context.Background()})
	require.NoError(t, err)
	require.Equal(t, "ok", out)
}

func TestToolDefExecuteDefaultTimeout(t *testing.T) {
	d := testTool("defaulted", 0, func(_ json.RawMessage, tc *ToolContext) (any, error) {
		deadline, hasDeadline := tc.Context.Deadline()
		require.True(t, hasDeadline)
		require.WithinDuration(t, time.Now().Add(DefaultToolTimeout), deadline, time.Minute)
		return "ok", nil
	})
	out, err := d.Execute(nil, &ToolContext{Context: context.Background()})
	require.NoError(t, err)
	require.Equal(t, "ok", out)
}

func TestToolDefExecuteNonPositiveArgOverrideIgnored(t *testing.T) {
	cases := map[string]time.Duration{
		"zero":     0,
		"negative": -1,
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			d := testTool("ignored-override", 50*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
				<-tc.Context.Done()
				return nil, tc.Context.Err()
			})
			d.TimeoutFromArgs = func(json.RawMessage) (time.Duration, bool) {
				return override, true
			}
			parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := d.Execute(nil, &ToolContext{Context: parent})
			require.Error(t, err)
			require.Contains(t, err.Error(), "timed out after 50ms")
		})
	}
}

func TestToolDefExecutePreservesHandlerDeadlineResult(t *testing.T) {
	d := testTool("graceful", 20*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
		<-tc.Context.Done()
		return map[string]any{"timed_out": true}, nil
	})
	out, err := d.Execute(nil, &ToolContext{Context: context.Background()})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"timed_out": true}, out)
}

func TestToolDefExecuteTimeoutFromArgs(t *testing.T) {
	d := testTool("argtimeout", 5*time.Second, func(_ json.RawMessage, tc *ToolContext) (any, error) {
		<-tc.Context.Done()
		return nil, tc.Context.Err()
	})
	d.TimeoutFromArgs = func(args json.RawMessage) (time.Duration, bool) {
		var req struct {
			TimeoutMs int `json:"timeout_ms"`
		}
		if json.Unmarshal(args, &req) != nil || req.TimeoutMs <= 0 {
			return 0, false
		}
		return time.Duration(req.TimeoutMs) * time.Millisecond, true
	}
	_, err := d.Execute(json.RawMessage(`{"timeout_ms":25}`), &ToolContext{Context: context.Background()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out after 25ms")
}

func TestToolDefExecuteParentDeadlineNotMasked(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		d := testTool("parent-cancelled", 50*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
			<-tc.Context.Done()
			return nil, tc.Context.Err()
		})
		_, err := d.Execute(nil, &ToolContext{Context: parent})
		require.ErrorIs(t, err, context.Canceled)
		require.NotContains(t, err.Error(), "timed out")
	})

	t.Run("expired", func(t *testing.T) {
		parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		d := testTool("parent-expired", 50*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
			<-tc.Context.Done()
			return nil, tc.Context.Err()
		})
		_, err := d.Execute(nil, &ToolContext{Context: parent})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NotContains(t, err.Error(), "timed out")
	})
}
