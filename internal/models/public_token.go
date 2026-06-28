package models

import "time"

type PublicToken struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Token        string    `json:"token"`
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by"`
}
