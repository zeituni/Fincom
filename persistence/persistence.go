package persistence

import (
	"alerts/alerts"
	"context"
)

type AlertStore interface {
	Create(ctx context.Context, alert *alerts.Alert) error
	GetByID(ctx context.Context, tenantID, id string) (*alerts.Alert, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*alerts.Alert, error)
	Update(ctx context.Context, alert *alerts.Alert) error
	Delete(ctx context.Context, tenantID, id string) error
}
