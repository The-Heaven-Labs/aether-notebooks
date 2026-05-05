package api

import (
	"context"
	"log"
	"time"
)

func (s *Server) CleanupOrphanAttachments(ctx context.Context) error {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT a.id
		FROM attachments a
		WHERE a.created_at < NOW() - INTERVAL '10 minutes'
		  AND NOT EXISTS (
		      SELECT 1 FROM cells c
		      WHERE c.notebook_id = a.notebook_id
		        AND c.source LIKE '%' || a.id::text || '%'
		  )
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Printf("attachment cleanup: scan: %v", err)
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	cleaned := 0
	for _, id := range ids {
		if err := s.store.Delete(id); err != nil {
			log.Printf("attachment cleanup: delete storage %s: %v", id, err)
		}
		if _, err := s.db.Pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id); err != nil {
			log.Printf("attachment cleanup: delete db %s: %v", id, err)
			continue
		}
		cleaned++
	}

	log.Printf("attachment cleanup: removed %d orphaned attachment(s)", cleaned)
	return nil
}

func (s *Server) StartBackgroundJobs(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.CleanupOrphanAttachments(ctx); err != nil {
					log.Printf("attachment cleanup: %v", err)
				}
			}
		}
	}()
}
