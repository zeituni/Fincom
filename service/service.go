package service

import (
	"alerts/alerts"
	"alerts/persistence"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// AlertService holds business logic for alert operations.
// It is the only layer that enforces domain rules such as write-once decisions.
type AlertService struct {
	store     persistence.AlertStore
	natsConn  *nats.Conn
	emitTopic string
}

func NewAlertService(store persistence.AlertStore) *AlertService {
	return &AlertService{
		store:     store,
		emitTopic: "alerts.events",
	}
}

// NewAlertServiceWithNATS creates an AlertService with NATS connection for event emission.
func NewAlertServiceWithNATS(store persistence.AlertStore, natsConn *nats.Conn) *AlertService {
	return &AlertService{
		store:     store,
		natsConn:  natsConn,
		emitTopic: "alerts.events",
	}
}

// SubmitDecision records a CLEARED or CONFIRMED_HIT outcome on an alert.
// It is write-once: if the alert is already in a terminal status the call
// returns ErrAlreadyDecided and the caller should respond with 409.
// Create accepts a new alert from an upstream screening system.
// Returns the persisted alert with server-generated id and timestamps.
// Initial status is OPEN.
func (svc *AlertService) Create(
	ctx context.Context,
	tenantID string,
	transactionID string,
	matchedEntityName string,
	matchScore int,
) (*alerts.Alert, error) {
	// --- input validation ---
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID is required", alerts.ErrInvalidInput)
	}
	if transactionID == "" {
		return nil, fmt.Errorf("%w: transactionID is required", alerts.ErrInvalidInput)
	}
	if matchedEntityName == "" {
		return nil, fmt.Errorf("%w: matchedEntityName is required", alerts.ErrInvalidInput)
	}
	if matchScore < 0 || matchScore > 100 {
		return nil, fmt.Errorf("%w: matchScore must be between 0 and 100", alerts.ErrInvalidInput)
	}

	// --- build alert ---
	alert := &alerts.Alert{
		ID:                uuid.New().String(),
		TransactionID:     transactionID,
		MatchedEntityName: matchedEntityName,
		MatchScore:        matchScore,
		Status:            alerts.StatusOpen,
		TenantID:          tenantID,
	}

	// --- persist ---
	log.Printf("Creating alert for tenant=%q, transactionID=%q, matchedEntityName=%q, matchScore=%d", tenantID, transactionID, matchedEntityName, matchScore)
	if err := svc.store.Create(ctx, alert); err != nil {
		return nil, err
	}
	return alert, nil
}

// List returns alerts for a tenant, optionally filtered by status and minimum matchScore.
func (svc *AlertService) List(
	ctx context.Context,
	tenantID string,
	status *alerts.Status,
	minMatchScore *int,
) ([]*alerts.Alert, error) {
	// --- input validation ---
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID is required", alerts.ErrInvalidInput)
	}

	// --- fetch all for tenant ---
	all, err := svc.store.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// --- apply optional filters --
	// With DB implementation the filterts shall be done in the DB
	log.Printf("ListAlerts: tenant=%q, status=%v, minMatchScore=%v", tenantID, status, minMatchScore)
	filtered := make([]*alerts.Alert, 0, len(all))
	for _, a := range all {
		if status != nil && a.Status != *status {
			continue
		}
		if minMatchScore != nil && a.MatchScore < *minMatchScore {
			continue
		}
		filtered = append(filtered, a)
	}

	return filtered, nil
}

// Escalate transitions an alert status to ESCALATED and emits a domain event.
// Only valid when current status is OPEN.
func (svc *AlertService) Escalate(
	ctx context.Context,
	tenantID string,
	alertID string,
) (*alerts.Alert, error) {
	// --- fetch alert ---
	alert, err := svc.store.GetByID(ctx, tenantID, alertID)
	if err != nil {
		return nil, err
	}

	// --- validate current status is OPEN ---
	if alert.Status != alerts.StatusOpen {
		return nil, fmt.Errorf("%w: current status is %q, must be OPEN",
			alerts.ErrInvalidStatus, alert.Status)
	}

	// --- transition to ESCALATED ---
	alert.Status = alerts.StatusEscalated

	if err := svc.store.Update(ctx, alert); err != nil {
		return nil, err
	}

	log.Printf("Emiting Alert %s as Escalated", alertID)
	svc.emitAlert(ctx, alert, "escalated") // best-effort async emit; errors logged internally

	return alert, nil
}

// EmitAlert publishes an alert to the NATS topic asynchronously.
// If NATS connection is not available, it logs and returns without error.
func (svc *AlertService) emitAlert(ctx context.Context, alert *alerts.Alert, decision string) {
	if svc.natsConn == nil {
		return // NATS not configured, skip emission
	}

	event := &alerts.DecisionEvent{
		Event:     "alert.decided",
		Decision:  decision, // e.g., "alert.escalated"
		AlertID:   alert.ID,
		TenantID:  ctx.Value("tenantID").(string),
		Timestamp: time.Now().UTC(),
	}

	// Run asynchronously in a goroutine
	go func() {
		// Marshal alert to JSON
		payload, err := json.Marshal(event)
		if err != nil {
			// In production, log this error properly
			fmt.Printf("failed to marshal alert %s: %v\n", alert.ID, err)
			return
		}

		// Publish to NATS topic with timeout
		publishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Use JetStream if available for persistence, otherwise use core NATS
		// For now, use simple Publish
		if err := svc.natsConn.Publish(svc.emitTopic, payload); err != nil {
			// In production, log this error properly and possibly add to dead letter queue
			fmt.Printf("failed to emit alert %s to NATS: %v\n", alert.ID, err)
		}

		// Ensure message is flushed
		if err := svc.natsConn.FlushWithContext(publishCtx); err != nil {
			fmt.Printf("failed to flush NATS connection for alert %s: %v\n", alert.ID, err)
		}
	}()
}

// SubmitDecision records a CLEARED or CONFIRMED_HIT outcome on an alert.
// It is write-once: if the alert is already in a terminal status the call
// returns ErrAlreadyDecided and the caller should respond with 409.
func (svc *AlertService) SubmitDecision(
	ctx context.Context,
	tenantID string,
	alertID string,
	status alerts.Status,
	note string,
) (*alerts.Alert, error) {
	// --- input validation ---
	if !alerts.IsDecisionStatus(status) {
		return nil, fmt.Errorf("%w: status must be CLEARED or CONFIRMED_HIT, got %q",
			alerts.ErrInvalidInput, status)
	}
	if note == "" {
		return nil, fmt.Errorf("%w: decisionNote is required", alerts.ErrInvalidInput)
	}

	// --- fetch (propagates ErrNotFound as-is) ---
	alert, err := svc.store.GetByID(ctx, tenantID, alertID)
	if err != nil {
		return nil, err
	}

	// --- write-once guard ---
	if alerts.IsTerminal(alert.Status) {
		return nil, fmt.Errorf("%w: current status is %q", alerts.ErrAlreadyDecided, alert.Status)
	}

	// --- apply decision ---
	alert.Status = status
	alert.DecisionNote = &note

	if err := svc.store.Update(ctx, alert); err != nil {
		return nil, err
	}
	// Decision successfully recorded; emit event asynchronously
	log.Printf("Emiting Alert %s as Decided", alert.ID)
	svc.emitAlert(ctx, alert, "decided") // best-effort async emit; errors logged internally

	return alert, nil
}
