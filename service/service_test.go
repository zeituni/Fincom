package service_test

import (
	"alerts/alerts"
	"alerts/persistence"
	"alerts/service"
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCreate_Success(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	got, err := svc.Create(context.Background(), "tenant-a", "tx-123", "Acme Corp", 85)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID == "" {
		t.Error("expected server-generated ID, got empty")
	}
	if got.TransactionID != "tx-123" {
		t.Errorf("transactionID: want tx-123, got %s", got.TransactionID)
	}
	if got.MatchedEntityName != "Acme Corp" {
		t.Errorf("matchedEntityName: want Acme Corp, got %s", got.MatchedEntityName)
	}
	if got.MatchScore != 85 {
		t.Errorf("matchScore: want 85, got %d", got.MatchScore)
	}
	if got.Status != alerts.StatusOpen {
		t.Errorf("status: want OPEN, got %s", got.Status)
	}
	if got.TenantID != "tenant-a" {
		t.Errorf("tenantID: want tenant-a, got %s", got.TenantID)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
	if got.DecisionNote != nil {
		t.Error("expected DecisionNote to be nil on creation")
	}
}

func TestCreate_MissingTenantID(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.Create(context.Background(), "", "tx-123", "Acme Corp", 85)
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_MissingTransactionID(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.Create(context.Background(), "tenant-a", "", "Acme Corp", 85)
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_MissingMatchedEntityName(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.Create(context.Background(), "tenant-a", "tx-123", "", 85)
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_InvalidMatchScore_Negative(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.Create(context.Background(), "tenant-a", "tx-123", "Acme Corp", -1)
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_InvalidMatchScore_ExceedsMax(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.Create(context.Background(), "tenant-a", "tx-123", "Acme Corp", 101)
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_ValidBoundaryScores(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	for _, score := range []int{0, 50, 100} {
		t.Run(fmt.Sprintf("score=%d", score), func(t *testing.T) {
			got, err := svc.Create(context.Background(), "tenant-a", "tx-123", "Acme Corp", score)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.MatchScore != score {
				t.Errorf("matchScore: want %d, got %d", score, got.MatchScore)
			}
		})
	}
}

// seed creates an alert directly via the store and returns its ID.
func seed(t *testing.T, store persistence.AlertStore, status alerts.Status) string {
	t.Helper()
	a := &alerts.Alert{
		TransactionID:     "tx-1",
		MatchedEntityName: "Acme Corp",
		MatchScore:        85,
		Status:            status,
		TenantID:          "tenant-a",
	}
	if err := store.Create(context.Background(), a); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return a.ID
}

func TestSubmitDecision_Success(t *testing.T) {
	for _, status := range []alerts.Status{alerts.StatusCleared, alerts.StatusConfirmedHit} {
		t.Run(string(status), func(t *testing.T) {
			store := persistence.NewMemoryStore()
			svc := service.NewAlertService(store)
			id := seed(t, store, alerts.StatusOpen)

			got, err := svc.SubmitDecision(context.Background(), "tenant-a", id, status, "looks clean")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != status {
				t.Errorf("status: want %s, got %s", status, got.Status)
			}
			if got.DecisionNote == nil || *got.DecisionNote != "looks clean" {
				t.Errorf("decisionNote not persisted correctly")
			}
		})
	}
}

func TestSubmitDecision_FromEscalated(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id := seed(t, store, alerts.StatusEscalated)

	_, err := svc.SubmitDecision(context.Background(), "tenant-a", id, alerts.StatusCleared, "reviewed")
	if err != nil {
		t.Fatalf("expected success from ESCALATED, got: %v", err)
	}
}

func TestSubmitDecision_AlreadyDecided(t *testing.T) {
	for _, terminal := range []alerts.Status{alerts.StatusCleared, alerts.StatusConfirmedHit} {
		t.Run(string(terminal), func(t *testing.T) {
			store := persistence.NewMemoryStore()
			svc := service.NewAlertService(store)
			id := seed(t, store, terminal)

			_, err := svc.SubmitDecision(context.Background(), "tenant-a", id, alerts.StatusCleared, "re-deciding")
			if !errors.Is(err, alerts.ErrAlreadyDecided) {
				t.Errorf("want ErrAlreadyDecided, got %v", err)
			}
		})
	}
}

func TestSubmitDecision_InvalidStatus(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id := seed(t, store, alerts.StatusOpen)

	_, err := svc.SubmitDecision(context.Background(), "tenant-a", id, alerts.StatusEscalated, "note")
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestSubmitDecision_EmptyNote(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id := seed(t, store, alerts.StatusOpen)

	_, err := svc.SubmitDecision(context.Background(), "tenant-a", id, alerts.StatusCleared, "")
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}
func TestList_Success(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	// Create multiple alerts
	id1, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)
	id2, _ := createAlert(t, svc, "tenant-a", "tx-2", "Entity B", 45)
	id3, _ := createAlert(t, svc, "tenant-a", "tx-3", "Entity C", 90)
	id4, _ := createAlert(t, svc, "tenant-b", "tx-4", "Entity D", 75) // different tenant

	got, err := svc.List(context.Background(), "tenant-a", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 alerts for tenant-a, got %d", len(got))
	}

	ids := make(map[string]bool)
	for _, a := range got {
		ids[a.ID] = true
	}
	if !ids[id1] || !ids[id2] || !ids[id3] {
		t.Error("expected to find all created alerts for tenant-a")
	}
	if ids[id4] {
		t.Error("should not include alert from tenant-b")
	}
}

func TestList_FilterByStatus(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	id1, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)
	id2, _ := createAlert(t, svc, "tenant-a", "tx-2", "Entity B", 45)
	svc.SubmitDecision(context.Background(), "tenant-a", id2, alerts.StatusCleared, "spam")

	open := alerts.StatusOpen
	got, err := svc.List(context.Background(), "tenant-a", &open, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 open alert, got %d", len(got))
	}
	if got[0].ID != id1 {
		t.Errorf("expected open alert to be %s, got %s", id1, got[0].ID)
	}
}

func TestList_FilterByMinMatchScore(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	id1, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)
	id2, _ := createAlert(t, svc, "tenant-a", "tx-2", "Entity B", 45)
	id3, _ := createAlert(t, svc, "tenant-a", "tx-3", "Entity C", 90)

	minScore := 80
	got, err := svc.List(context.Background(), "tenant-a", nil, &minScore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 alerts with score >= 80, got %d", len(got))
	}

	ids := make(map[string]bool)
	for _, a := range got {
		ids[a.ID] = true
	}
	if !ids[id1] || !ids[id3] {
		t.Error("expected to find alerts with score >= 80")
	}
	if ids[id2] {
		t.Error("should not include alert with score 45")
	}
}

func TestList_FilterByStatusAndMinMatchScore(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	id1, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)
	//id2, _ := createAlert(t, svc, "tenant-a", "tx-2", "Entity B", 45)
	id3, _ := createAlert(t, svc, "tenant-a", "tx-3", "Entity C", 90)
	svc.SubmitDecision(context.Background(), "tenant-a", id3, alerts.StatusCleared, "spam")

	open := alerts.StatusOpen
	minScore := 80
	got, err := svc.List(context.Background(), "tenant-a", &open, &minScore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 alert, got %d", len(got))
	}
	if got[0].ID != id1 {
		t.Errorf("expected alert to be %s, got %s", id1, got[0].ID)
	}
}

func TestList_EmptyResult(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	got, err := svc.List(context.Background(), "tenant-a", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d alerts", len(got))
	}
}

func TestList_MissingTenantID(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.List(context.Background(), "", nil, nil)
	if !errors.Is(err, alerts.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

// createAlert is a helper to create an alert and return its ID.
func createAlert(t *testing.T, svc *service.AlertService, tenantID, txID, entityName string, score int) (string, *alerts.Alert) {
	t.Helper()
	a, err := svc.Create(context.Background(), tenantID, txID, entityName, score)
	if err != nil {
		t.Fatalf("createAlert: %v", err)
	}
	return a.ID, a
}

func TestEscalate_Success(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)

	got, err := svc.Escalate(context.Background(), "tenant-a", id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != alerts.StatusEscalated {
		t.Errorf("status: want ESCALATED, got %s", got.Status)
	}

}

func TestEscalate_NotFound(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.Escalate(context.Background(), "tenant-a", "no-such-id")
	if !errors.Is(err, alerts.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestEscalate_InvalidStatus_AlreadyEscalated(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)

	// First escalation succeeds
	svc.Escalate(context.Background(), "tenant-a", id)

	// Second escalation fails
	_, err := svc.Escalate(context.Background(), "tenant-a", id)
	if !errors.Is(err, alerts.ErrInvalidStatus) {
		t.Errorf("want ErrInvalidStatus, got %v", err)
	}
}

func TestEscalate_InvalidStatus_Cleared(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)

	// Decide the alert
	svc.SubmitDecision(context.Background(), "tenant-a", id, alerts.StatusCleared, "spam")

	// Escalation should fail
	_, err := svc.Escalate(context.Background(), "tenant-a", id)
	if !errors.Is(err, alerts.ErrInvalidStatus) {
		t.Errorf("want ErrInvalidStatus, got %v", err)
	}
}

func TestEscalate_TenantIsolation(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id, _ := createAlert(t, svc, "tenant-a", "tx-1", "Entity A", 85)

	// tenant-b should not be able to escalate tenant-a's alert
	_, err := svc.Escalate(context.Background(), "tenant-b", id)
	if !errors.Is(err, alerts.ErrNotFound) {
		t.Errorf("want ErrNotFound for wrong tenant, got %v", err)
	}
}

func TestSubmitDecision_NotFound(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)

	_, err := svc.SubmitDecision(context.Background(), "tenant-a", "no-such-id", alerts.StatusCleared, "note")
	if !errors.Is(err, alerts.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestSubmitDecision_TenantIsolation(t *testing.T) {
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	id := seed(t, store, alerts.StatusOpen) // seeded under tenant-a

	// tenant-b should not see tenant-a's alert
	_, err := svc.SubmitDecision(context.Background(), "tenant-b", id, alerts.StatusCleared, "note")
	if !errors.Is(err, alerts.ErrNotFound) {
		t.Errorf("want ErrNotFound for wrong tenant, got %v", err)
	}
}
