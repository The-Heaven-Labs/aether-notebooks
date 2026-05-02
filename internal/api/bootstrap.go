package api

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedPlatformAdmin promotes the given email to platform admin if the user exists.
// Returns true if a user was promoted, false if the user doesn't exist yet.
// The caller should call SetPlatformAdminEmail so the promotion happens at
// registration instead if the user doesn't exist.
func SeedPlatformAdmin(ctx context.Context, pool *pgxpool.Pool, email string) (bool, error) {
	if email == "" {
		return false, nil
	}
	tag, err := pool.Exec(ctx,
		`UPDATE users SET is_platform_admin=true WHERE email=$1`, email)
	if err != nil {
		return false, fmt.Errorf("seed platform admin: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
