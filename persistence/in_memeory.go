package persistence

import (
	"alerts/alerts"
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// tenantShard holds all alerts for a single tenant under its own lock.
type tenantShard struct {
	mu     sync.RWMutex
	alerts map[string]*alerts.Alert
}

type MemoryStore struct {
	mu     sync.RWMutex            // protects the shards map itself
	shards map[string]*tenantShard // keyed by tenantID
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		shards: make(map[string]*tenantShard),
	}
}

// shard returns (creating if necessary) the shard for a tenant.
func (s *MemoryStore) shard(tenantID string) *tenantShard {
	s.mu.RLock()
	sh, ok := s.shards[tenantID]
	s.mu.RUnlock()
	if ok {
		return sh
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Double-checked locking — another goroutine may have beaten us.
	if sh, ok = s.shards[tenantID]; ok {
		return sh
	}
	sh = &tenantShard{alerts: make(map[string]*alerts.Alert)}
	s.shards[tenantID] = sh
	return sh
}

func (s *MemoryStore) Create(_ context.Context, a *alerts.Alert) error {
	if a.TenantID == "" {
		return fmt.Errorf("tenantID is required")
	}
	now := time.Now().UTC()
	a.ID = uuid.NewString()
	a.CreatedAt = now
	a.UpdatedAt = now

	sh := s.shard(a.TenantID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	// Store a copy so callers can't mutate cached state via the pointer.
	clone := *a
	sh.alerts[a.ID] = &clone
	return nil
}

func (s *MemoryStore) GetByID(_ context.Context, tenantID, id string) (*alerts.Alert, error) {
	sh := s.shard(tenantID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	a, ok := sh.alerts[id]
	if !ok {
		log.Printf("GetByID: alert %q not found for tenant %q", id, tenantID)
		return nil, alerts.ErrNotFound
	}
	clone := *a
	return &clone, nil
}

func (s *MemoryStore) ListByTenant(_ context.Context, tenantID string) ([]*alerts.Alert, error) {
	sh := s.shard(tenantID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	out := make([]*alerts.Alert, 0, len(sh.alerts))
	for _, a := range sh.alerts {
		clone := *a
		out = append(out, &clone)
	}
	return out, nil
}

func (s *MemoryStore) Update(_ context.Context, a *alerts.Alert) error {
	sh := s.shard(a.TenantID)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if _, ok := sh.alerts[a.ID]; !ok {
		log.Printf("Update: alert %q not found for tenant %q", a.ID, a.TenantID)
		return alerts.ErrNotFound
	}
	a.UpdatedAt = time.Now().UTC()
	clone := *a
	sh.alerts[a.ID] = &clone
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, tenantID, id string) error {
	sh := s.shard(tenantID)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if _, ok := sh.alerts[id]; !ok {
		log.Printf("Delete: alert %q not found for tenant %q", id, tenantID)
		return alerts.ErrNotFound
	}
	delete(sh.alerts, id)
	return nil
}
