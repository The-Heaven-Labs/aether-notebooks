package agent

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reearth/ygo/crdt"
)

// UpdateCellInYjs updates a cell's source content in the Yjs document.
// Yjs is the source of truth; cells.source is a derived cache.
func UpdateCellInYjs(ctx context.Context, db *pgxpool.Pool, notebookID, cellID, newSource string) error {
	// 1. Load current Yjs state (may not exist yet)
	var state []byte
	err := db.QueryRow(ctx,
		"SELECT state FROM yjs_documents WHERE notebook_id = $1",
		notebookID,
	).Scan(&state)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("load yjs state: %w", err)
	}

	// 2. Create/decode Yjs document
	doc := crdt.New()
	if len(state) > 0 {
		if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
			return fmt.Errorf("decode yjs state: %w", err)
		}
	}

	// 3. Update the cell's text content in Yjs
	//    Key pattern: "cell:{cellID}" — matches frontend convention
	ytext := doc.GetText("cell:" + cellID)
	existing := ytext.ToString()
	if existing == newSource {
		// No change needed
		return nil
	}

	doc.Transact(func(txn *crdt.Transaction) {
		if existing != "" {
			ytext.Delete(txn, 0, len(existing))
		}
		if newSource != "" {
			ytext.Insert(txn, 0, newSource, nil)
		}
	})

	// 4. Encode the updated state
	newState := doc.EncodeStateAsUpdate()

	// 5. Store back to database (upsert)
	_, err = db.Exec(ctx,
		`INSERT INTO yjs_documents (notebook_id, state, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (notebook_id) DO UPDATE SET state = $2, updated_at = NOW()`,
		notebookID, newState,
	)
	if err != nil {
		return fmt.Errorf("save yjs state: %w", err)
	}

	return nil
}

// DecodeYjsState loads and decodes a Yjs document from the database.
// Used in tests to verify Yjs state.
func DecodeYjsState(db *pgxpool.Pool, notebookID string) (*crdt.Doc, error) {
	var state []byte
	err := db.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1",
		notebookID,
	).Scan(&state)
	if err != nil {
		return nil, fmt.Errorf("load yjs state: %w", err)
	}

	doc := crdt.New()
	if len(state) > 0 {
		if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
			return nil, fmt.Errorf("decode yjs state: %w", err)
		}
	}
	return doc, nil
}
