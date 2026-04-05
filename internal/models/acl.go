package models

import "time"

type ACLEntry struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	SubjectType  string    `json:"subject_type"`
	SubjectID    string    `json:"subject_id"`
	Actions      []string  `json:"actions"`
	CreatedAt    time.Time `json:"created_at"`
}
