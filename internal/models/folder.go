package models

import "time"

type Folder struct {
	ID        string     `json:"id"`
	OrgID     string     `json:"org_id"`
	ParentID  *string    `json:"parent_id,omitempty"`
	Name      string     `json:"name"`
	IsHome    bool       `json:"is_home"`
	OwnerID   *string    `json:"owner_id,omitempty"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}
