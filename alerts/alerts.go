package alerts

import (
	"errors"
	"time"
)

type Status string

const (
	StatusOpen         Status = "OPEN"
	StatusEscalated    Status = "ESCALATED"
	StatusCleared      Status = "CLEARED"
	StatusConfirmedHit Status = "CONFIRMED_HIT"
)

var (
	ErrNotFound       = errors.New("alert not found")
	ErrAlreadyDecided = errors.New("alert already decided")
	ErrInvalidInput   = errors.New("invalid input")
	ErrInvalidStatus  = errors.New("invalid status transition")
)

// DecisionEvent represents an event emitted by the alert domain.
type DecisionEvent struct {
	Event     string
	Decision  string // e.g., "alert.escalated"
	AlertID   string
	TenantID  string
	Timestamp time.Time
}

// decisionStatuses is the closed set of terminal statuses.
var decisionStatuses = map[Status]bool{
	StatusCleared:      true,
	StatusConfirmedHit: true,
}

// IsDecisionStatus reports whether s is a valid decision outcome.
func IsDecisionStatus(s Status) bool {
	return decisionStatuses[s]
}

// IsTerminal reports whether an alert in status s can no longer be decided.
func IsTerminal(s Status) bool {
	return decisionStatuses[s]
}

type Alert struct {
	ID                string    `json:"id" db:"id"`
	TransactionID     string    `json:"transaction_id" db:"transaction_id"`
	MatchedEntityName string    `json:"matched_entity_name" db:"matched_entity_name"`
	MatchScore        int       `json:"match_score" db:"match_score"` // 0–100
	Status            Status    `json:"status" db:"status"`
	AssignedTo        *string   `json:"assigned_to" db:"assigned_to"` // optional
	TenantID          string    `json:"tenant_id" db:"tenant_id"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
	DecisionNote      *string   `json:"decision_note" db:"decision_note"` // required on decision, absent otherwise
}
