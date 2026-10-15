package cli

// Tests for the folder listing paging loop: the API serves a first page per
// section (default limit 50), so the CLI must walk every section reporting
// has_more through the load-more endpoint until each is complete — while
// keeping the printed map shape the server returned.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// requestRecorder collects request URIs in order across handler goroutines.
type requestRecorder struct {
	mu   sync.Mutex
	uris []string
}

func (r *requestRecorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uris = append(r.uris, req.URL.RequestURI())
}

func (r *requestRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.uris...)
}

func writeJSONBody(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func TestGetFolderContentsPagesThroughSections(t *testing.T) {
	var recorder requestRecorder

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/folders/folder-1", func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSONBody(w, `{
			"folder": {"id": "folder-1"},
			"folders": [{"id": "sub-1"}],
			"notebooks": [{"id": "nb-1"}, {"id": "nb-2"}],
			"connectors": [{"id": "c-1"}],
			"dashboards": [],
			"totals": {"notebooks": 3, "connectors": 1, "dashboards": 0},
			"has_more": {"notebooks": true, "connectors": false, "dashboards": false}
		}`)
	})
	mux.HandleFunc("GET /api/v1/folders/folder-1/contents", func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSONBody(w, `{"items": [{"id": "nb-3"}], "total": 3, "has_more": false, "offset": 2}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	result, err := client.GetFolderContents("folder-1")
	require.NoError(t, err)

	notebooks, ok := result["notebooks"].([]interface{})
	require.True(t, ok)
	ids := make([]string, 0, len(notebooks))
	for _, item := range notebooks {
		ids = append(ids, item.(map[string]interface{})["id"].(string))
	}
	require.Equal(t, []string{"nb-1", "nb-2", "nb-3"}, ids, "pages must append in order")

	require.Len(t, result["connectors"].([]interface{}), 1)
	require.Len(t, result["folders"].([]interface{}), 1, "non-paginated keys must pass through untouched")
	require.Equal(t, map[string]interface{}{"notebooks": float64(3), "connectors": float64(1), "dashboards": float64(0)},
		result["totals"], "totals keep the server's exact counts")
	require.False(t, result["has_more"].(map[string]interface{})["notebooks"].(bool),
		"a completed section reports has_more false")

	require.Equal(t, []string{
		"/api/v1/folders/folder-1?limit=200",
		"/api/v1/folders/folder-1/contents?type=notebook&offset=2&limit=200",
	}, recorder.snapshot(), "only the incomplete section is paged, from the loaded offset")
}

func TestListRootContentsPagesThroughSections(t *testing.T) {
	var recorder requestRecorder

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/folders", func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSONBody(w, `{
			"folders": [],
			"notebooks": [],
			"connectors": [{"id": "c-1"}],
			"dashboards": [],
			"totals": {"notebooks": 0, "connectors": 4, "dashboards": 0},
			"has_more": {"notebooks": false, "connectors": true, "dashboards": false}
		}`)
	})
	mux.HandleFunc("GET /api/v1/folders/contents", func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		switch r.URL.Query().Get("offset") {
		case "1":
			writeJSONBody(w, `{"items": [{"id": "c-2"}, {"id": "c-3"}], "total": 4, "has_more": true, "offset": 1}`)
		case "3":
			writeJSONBody(w, `{"items": [{"id": "c-4"}], "total": 4, "has_more": false, "offset": 3}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	result, err := client.ListRootContents()
	require.NoError(t, err)

	connectors := result["connectors"].([]interface{})
	require.Len(t, connectors, 4, "the whole connector section must be loaded")
	require.Equal(t, "c-4", connectors[3].(map[string]interface{})["id"])
	require.False(t, result["has_more"].(map[string]interface{})["connectors"].(bool))

	require.Equal(t, []string{
		"/api/v1/folders?limit=200",
		"/api/v1/folders/contents?type=connector&offset=1&limit=200",
		"/api/v1/folders/contents?type=connector&offset=3&limit=200",
	}, recorder.snapshot(), "the loop must continue until has_more turns false")
}

func TestListRootContentsWithoutHasMoreSkipsPaging(t *testing.T) {
	var recorder requestRecorder

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/folders", func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		// Pre-pagination server shape: no has_more key.
		writeJSONBody(w, `{"folders": [], "notebooks": [{"id": "nb-1"}], "connectors": [], "dashboards": []}`)
	})
	mux.HandleFunc("GET /api/v1/folders/contents", func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		w.WriteHeader(http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	result, err := client.ListRootContents()
	require.NoError(t, err)
	require.Len(t, result["notebooks"].([]interface{}), 1)
	_, hasHasMore := result["has_more"]
	require.False(t, hasHasMore, "the CLI must not invent keys the server did not send")

	require.Equal(t, []string{"/api/v1/folders?limit=200"}, recorder.snapshot())
}

func TestGetFolderContentsPagingStallFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/folders/folder-1", func(w http.ResponseWriter, r *http.Request) {
		writeJSONBody(w, `{
			"folders": [], "notebooks": [{"id": "nb-1"}], "connectors": [], "dashboards": [],
			"totals": {"notebooks": 2, "connectors": 0, "dashboards": 0},
			"has_more": {"notebooks": true, "connectors": false, "dashboards": false}
		}`)
	})
	mux.HandleFunc("GET /api/v1/folders/folder-1/contents", func(w http.ResponseWriter, r *http.Request) {
		// has_more with no items would loop forever; the CLI must fail instead.
		writeJSONBody(w, `{"items": [], "total": 2, "has_more": true, "offset": 1}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.GetFolderContents("folder-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stalled")
}
