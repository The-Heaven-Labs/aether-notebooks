package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/api"
)

// setupOutputLimitsFixture registers an org (the user is its admin), creates a
// notebook + cell, and stores the given raw outputs JSON on the cell. Returns
// the server, token, notebook ID, cell ID, and the stored outputs.
func setupOutputLimitsFixture(t *testing.T, outputs string) (*api.Server, string, string, string, string) {
	t.Helper()
	srv := setupTestServer(t)
	email := fmt.Sprintf("outlimits-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Output Limits Org")
	nbID := createNotebook(t, srv, token, "Output Limits NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", "")

	if outputs != "" {
		_, err := srv.DB().Pool.Exec(t.Context(),
			`UPDATE cells SET outputs = $1 WHERE id = $2`, outputs, cellID)
		if err != nil {
			t.Fatalf("seed outputs: %v", err)
		}
	}
	return srv, token, nbID, cellID, outputs
}

func getNotebookBody(t *testing.T, srv *api.Server, token, nbID string) []byte {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET notebook: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// decodeCells extracts the raw cells array from a notebook GET response body.
func decodeCells(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	var payload struct {
		Cells []json.RawMessage `json:"cells"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload.Cells
}

// cellOutputsRaw pulls the raw "outputs" JSON from a single cell object.
func cellOutputsRaw(t *testing.T, cell json.RawMessage) json.RawMessage {
	t.Helper()
	var c struct {
		Outputs json.RawMessage `json:"outputs"`
	}
	if err := json.Unmarshal(cell, &c); err != nil {
		t.Fatalf("decode cell: %v", err)
	}
	return c.Outputs
}

// TestCellOutputsByteIdentity verifies the read path is a pass-through: the
// outputs embedded in the notebook GET response equal the stored column modulo
// lossless JSON whitespace compaction. Go's encoding/json compacts
// json.RawMessage on marshal, but it never decodes into []any, so there is no
// ~10x heap amplification. (Note the stored column is JSONB, which Postgres
// canonicalizes on write.)
func TestCellOutputsByteIdentity(t *testing.T) {
	srv, token, nbID, _, _ := setupOutputLimitsFixture(t, `[{"type":"table","data":{"columns":[{"name":"a","type":"string"}],"rows":[["x"],["y"]]}}]`)

	var stored []byte
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT outputs FROM cells WHERE notebook_id = $1`, nbID).Scan(&stored); err != nil {
		t.Fatalf("read stored outputs: %v", err)
	}
	var compactStored bytes.Buffer
	if err := json.Compact(&compactStored, stored); err != nil {
		t.Fatalf("compact stored: %v", err)
	}

	body := getNotebookBody(t, srv, token, nbID)
	cells := decodeCells(t, body)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	got := cellOutputsRaw(t, cells[0])
	if !bytes.Equal(got, compactStored.Bytes()) {
		t.Errorf("outputs not equal to compacted stored column:\ngot:  %s\nwant: %s", got, compactStored.Bytes())
	}
}

// TestNotebookInlineBudgetStubsOverflow verifies the read-path budget: when the
// total inline bytes exceed the org budget, cells are walked in position order
// and the ones that no longer fit are replaced with the truncation stub.
func TestNotebookInlineBudgetStubsOverflow(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("outlimits-budget-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Budget Org")
	nbID := createNotebook(t, srv, token, "Budget NB")
	srv.SetOutputLimitsMaxBytes(128 * 1024 * 1024)

	bigA := `[{"type":"table","data":{"columns":[{"name":"a","type":"string"}],"rows":[["aaaa"]]}}]`
	bigB := `[{"type":"table","data":{"columns":[{"name":"b","type":"string"}],"rows":[["bbbb"]]}}]`
	cellA := createCell(t, srv, token, nbID, "sql", "SELECT 2", "")
	cellB := createCell(t, srv, token, nbID, "sql", "SELECT 3", "")
	if _, err := srv.DB().Pool.Exec(t.Context(), `UPDATE cells SET outputs=$1 WHERE id=$2`, bigA, cellA); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB().Pool.Exec(t.Context(), `UPDATE cells SET outputs=$1 WHERE id=$2`, bigB, cellB); err != nil {
		t.Fatal(err)
	}
	var storedA []byte
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT outputs FROM cells WHERE id=$1`, cellA).Scan(&storedA); err != nil {
		t.Fatal(err)
	}
	var sizeA, sizeB int64
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT pg_column_size(outputs) FROM cells WHERE id=$1`, cellA).Scan(&sizeA); err != nil {
		t.Fatal(err)
	}
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT pg_column_size(outputs) FROM cells WHERE id=$1`, cellB).Scan(&sizeB); err != nil {
		t.Fatal(err)
	}

	// Budget exactly large enough for the first cell (position 0) but not both.
	if _, err := srv.DB().Pool.Exec(t.Context(),
		`UPDATE orgs SET notebook_inline_outputs_max_bytes = $1 WHERE id = (SELECT org_id FROM notebooks WHERE id=$2)`, sizeA, nbID); err != nil {
		t.Fatal(err)
	}

	body := getNotebookBody(t, srv, token, nbID)
	cells := decodeCells(t, body)
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(cells))
	}

	// First cell (position 0) is inlined verbatim (modulo JSON compaction).
	first := cellOutputsRaw(t, cells[0])
	var compactA bytes.Buffer
	if err := json.Compact(&compactA, storedA); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, compactA.Bytes()) {
		t.Errorf("first cell should be inlined, got %s (sizeA=%d sizeB=%d)", first, sizeA, sizeB)
	}

	// Second cell is stubbed.
	second := cellOutputsRaw(t, cells[1])
	var out []struct {
		Type string `json:"type"`
		Data struct {
			Truncated bool  `json:"truncated"`
			Bytes     int64 `json:"bytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(second, &out); err != nil {
		t.Fatalf("stub should be valid JSON: %v", err)
	}
	if len(out) != 1 || out[0].Type != "table" || !out[0].Data.Truncated || out[0].Data.Bytes <= 0 {
		t.Errorf("expected truncation stub with positive bytes, got %+v", out)
	}
}

// TestNotebookInlineBudgetAllInlined verifies the budget default (32MB) inlines
// every output when the total fits.
func TestNotebookInlineBudgetAllInlined(t *testing.T) {
	srv, token, nbID, _, _ := setupOutputLimitsFixture(t, `[{"type":"table","data":{"rows":[[1]]}}]`)

	var stored []byte
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT outputs FROM cells WHERE notebook_id = $1`, nbID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var compactStored bytes.Buffer
	if err := json.Compact(&compactStored, stored); err != nil {
		t.Fatal(err)
	}

	body := getNotebookBody(t, srv, token, nbID)
	cells := decodeCells(t, body)
	got := cellOutputsRaw(t, cells[0])
	if !bytes.Equal(got, compactStored.Bytes()) {
		t.Errorf("expected all outputs inlined, got %s", got)
	}
}

// TestCellOutputsDownload verifies the streaming download endpoint: byte
// identity, Content-Disposition, and the same view permission as the cell.
func TestCellOutputsDownload(t *testing.T) {
	srv, token, _, cellID, _ := setupOutputLimitsFixture(t, `[{"type":"table","data":{"columns":[{"name":"a","type":"string"}],"rows":[["x"]]}}]`)

	var stored []byte
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT outputs FROM cells WHERE id=$1`, cellID).Scan(&stored); err != nil {
		t.Fatalf("read stored outputs: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/cells/"+cellID+"/outputs/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="cell-`) || !strings.HasSuffix(cd, `.json"`) {
		t.Errorf("expected attachment Content-Disposition, got %q", cd)
	}
	if !bytes.Equal(rec.Body.Bytes(), stored) {
		t.Errorf("download body not byte-identical to stored column:\ngot:  %s\nwant: %s", rec.Body.String(), stored)
	}
}

// TestCellOutputsDownloadUnauthorized verifies a user without view permission on
// the notebook cannot download a cell's outputs.
func TestCellOutputsDownloadUnauthorized(t *testing.T) {
	srv, _, _, cellID, _ := setupOutputLimitsFixture(t, `[]`)
	// A user in a different org has no permission.
	otherEmail := fmt.Sprintf("outlimits-other-%d@example.com", time.Now().UnixNano())
	otherToken := registerAndGetToken(t, srv, otherEmail, "Other Org")

	req := httptest.NewRequest("GET", "/api/v1/cells/"+cellID+"/outputs/download", nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("expected 404/403 for unauthorized download, got %d", rec.Code)
	}
}

// TestOrgOutputLimitsRoundTrip verifies GET/PUT of the org output limits and
// that 0 means unlimited.
func TestOrgOutputLimitsRoundTrip(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("outlimits-settings-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Settings Org")

	get := func() map[string]int64 {
		req := httptest.NewRequest("GET", "/api/v1/org/output-limits", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET: expected 200, got %d", rec.Code)
		}
		var out map[string]int64
		json.NewDecoder(rec.Body).Decode(&out)
		return out
	}

	initial := get()
	if initial["cell_output_max_bytes"] != 10485760 {
		t.Errorf("expected default cell cap 10MB, got %d", initial["cell_output_max_bytes"])
	}
	if initial["notebook_inline_outputs_max_bytes"] != 33554432 {
		t.Errorf("expected default inline budget 32MB, got %d", initial["notebook_inline_outputs_max_bytes"])
	}
	// Default platform ceiling is 0 (unset in tests) → no clamping.
	if initial["cell_output_max_bytes_resolved"] != 10485760 {
		t.Errorf("expected resolved == stored without a ceiling, got %d", initial["cell_output_max_bytes_resolved"])
	}

	// Set cell cap to 0 (unlimited) and inline to 1MB.
	body, _ := json.Marshal(map[string]any{"cell_output_max_bytes": 0, "notebook_inline_outputs_max_bytes": 1048576})
	req := httptest.NewRequest("PUT", "/api/v1/org/output-limits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	after := get()
	if after["cell_output_max_bytes"] != 0 {
		t.Errorf("expected cell cap 0 (unlimited), got %d", after["cell_output_max_bytes"])
	}
	if after["cell_output_max_bytes_resolved"] != 0 {
		t.Errorf("expected resolved 0 (unlimited), got %d", after["cell_output_max_bytes_resolved"])
	}
	if after["notebook_inline_outputs_max_bytes"] != 1048576 {
		t.Errorf("expected inline 1MB, got %d", after["notebook_inline_outputs_max_bytes"])
	}
}

// TestOrgOutputLimitsPlatformClamp verifies the platform ceiling clamps the
// resolved values returned to the UI.
func TestOrgOutputLimitsPlatformClamp(t *testing.T) {
	srv := setupTestServer(t)
	srv.SetOutputLimitsMaxBytes(64 * 1024 * 1024)
	email := fmt.Sprintf("outlimits-clamp-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Clamp Org")

	// Set cell cap to 128MB (> 64MB ceiling).
	body, _ := json.Marshal(map[string]any{"cell_output_max_bytes": 128 * 1024 * 1024})
	req := httptest.NewRequest("PUT", "/api/v1/org/output-limits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d", rec.Code)
	}

	var out map[string]int64
	json.NewDecoder(rec.Body).Decode(&out)
	if out["cell_output_max_bytes"] != 128*1024*1024 {
		t.Errorf("stored value should remain 128MB, got %d", out["cell_output_max_bytes"])
	}
	if out["cell_output_max_bytes_resolved"] != 64*1024*1024 {
		t.Errorf("resolved value should clamp to 64MB, got %d", out["cell_output_max_bytes_resolved"])
	}
	if out["platform_max_bytes"] != 64*1024*1024 {
		t.Errorf("expected platform ceiling 64MB, got %d", out["platform_max_bytes"])
	}
}

// TestOrgOutputLimitsValidation verifies non-negative validation.
func TestOrgOutputLimitsValidation(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("outlimits-validation-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Validation Org")

	body, _ := json.Marshal(map[string]any{"cell_output_max_bytes": -5})
	req := httptest.NewRequest("PUT", "/api/v1/org/output-limits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative value: expected 400, got %d", rec.Code)
	}

	req = httptest.NewRequest("PUT", "/api/v1/org/output-limits", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body: expected 400, got %d", rec.Code)
	}
}

// TestExecuteCellTruncatesOversizedOutput verifies the executor byte cap
// end-to-end: a query producing more bytes than the org's cell cap stores a
// truncated table output on the cell.
func TestExecuteCellTruncatesOversizedOutput(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("outlimits-exec-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Exec Limits Org")
	connID := createConnector(t, srv, token)
	nbID := createNotebook(t, srv, token, "Exec Limits NB")
	cellID := createCell(t, srv, token, nbID, "sql", `SELECT i, repeat('x', 1000) AS payload FROM generate_series(1, 10) AS i`, connID)

	// Shrink the org's per-cell cap so the query's output exceeds it.
	if _, err := srv.DB().Pool.Exec(t.Context(),
		`UPDATE orgs SET cell_output_max_bytes = 2500 WHERE id = (SELECT org_id FROM notebooks WHERE id=$1)`, nbID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/execute", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The stored output must carry the truncation markers.
	var outputs []byte
	if err := srv.DB().Pool.QueryRow(t.Context(), `SELECT outputs FROM cells WHERE id=$1`, cellID).Scan(&outputs); err != nil {
		t.Fatal(err)
	}
	var out []struct {
		Type string `json:"type"`
		Data struct {
			Truncated    bool  `json:"truncated"`
			RowsIncluded int   `json:"rows_included"`
			RowsTotal    int64 `json:"rows_total"`
			Bytes        int64 `json:"bytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(outputs, &out); err != nil {
		t.Fatalf("stored outputs not valid JSON: %v", err)
	}
	if len(out) != 1 || out[0].Type != "table" {
		t.Fatalf("expected a single table output, got %+v", out)
	}
	if !out[0].Data.Truncated {
		t.Error("expected truncated=true on the stored output")
	}
	if out[0].Data.RowsIncluded == 0 {
		t.Error("expected at least one row within a 2500-byte budget")
	}
	if out[0].Data.RowsTotal != -1 {
		t.Errorf("expected RowsTotal=-1 for postgres, got %d", out[0].Data.RowsTotal)
	}
	if out[0].Data.Bytes <= 0 {
		t.Errorf("expected positive estimated bytes, got %d", out[0].Data.Bytes)
	}

	// GET the notebook: the truncated output should be inlined (it is now small).
	body := getNotebookBody(t, srv, token, nbID)
	cells := decodeCells(t, body)
	got := cellOutputsRaw(t, cells[0])
	if !bytes.Contains(got, []byte(`"truncated":true`)) {
		t.Errorf("expected truncated flag in notebook GET output, got %s", got)
	}
}

// TestNotebookGetMemoryBounded stores a large output row and asserts the
// notebook GET handler does not amplify it into `[]any` (~10x). The RawMessage
// pass-through keeps allocations within a small multiple of the raw bytes.
func TestNotebookGetMemoryBounded(t *testing.T) {
	// Build a ~8MB stored output (a single JSON array of long strings).
	chunk := strings.Repeat("x", 8192)
	var sb strings.Builder
	sb.WriteString(`[{"type":"table","data":{"columns":[{"name":"a","type":"string"}],"rows":[`)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`["` + chunk + fmt.Sprintf(`%06d"]`, i))
	}
	sb.WriteString(`]}}]`)
	stored := sb.String()

	srv, token, nbID, _, _ := setupOutputLimitsFixture(t, stored)

	// Warm up (JIT, pool) outside the measurement.
	getNotebookBody(t, srv, token, nbID)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	body := getNotebookBody(t, srv, token, nbID)
	runtime.ReadMemStats(&after)

	allocated := int64(after.TotalAlloc - before.TotalAlloc)
	raw := int64(len(stored))
	// The new path is a pass-through: DB scan (raw) + response encode (raw)
	// should stay well under 5x. The old `json.Unmarshal` into []any amplified
	// to ~10x.
	if allocated > raw*5 {
		t.Errorf("notebook GET allocated %d bytes for a %d-byte output (%.1fx); want <= 5x",
			allocated, raw, float64(allocated)/float64(raw))
	}
	_ = body
}
