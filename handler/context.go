package handler

import (
	"context"
	"fmt"
)

type contextKey string

const tenantContextKey contextKey = "tenantID"

// WithTenantID stores a tenantID in the context (called by auth middleware).
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey, tenantID)
}

// TenantIDFromContext retrieves the tenantID set by middleware.
// Returns an error if the value is absent or empty, which the handler
// should surface as 403 (middleware failure) rather than 400.
func TenantIDFromContext(ctx context.Context) (string, error) {
	v, ok := ctx.Value(tenantContextKey).(string)
	if !ok || v == "" {
		return "", fmt.Errorf("tenantID missing from context")
	}
	return v, nil
}
