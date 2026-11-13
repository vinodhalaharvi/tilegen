// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package orders

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// MemoryOrderStore implements OrderStore using memory storage.
type MemoryOrderStore struct {
	mu sync.Mutex
	m  map[uuid.UUID]*Order
}

// NewMemoryOrderStore returns a ready-to-use MemoryOrderStore.
func NewMemoryOrderStore() *MemoryOrderStore {
	return &MemoryOrderStore{m: make(map[uuid.UUID]*Order)}
}

func (s *MemoryOrderStore) Get(ctx context.Context, id uuid.UUID) (*Order, error) {
	panic("tilegen:hole orders.MemoryOrderStore.Get")
}

func (s *MemoryOrderStore) List(ctx context.Context) ([]*Order, error) {
	panic("tilegen:hole orders.MemoryOrderStore.List")
}

func (s *MemoryOrderStore) Save(ctx context.Context, order *Order) error {
	panic("tilegen:hole orders.MemoryOrderStore.Save")
}

func (s *MemoryOrderStore) Delete(ctx context.Context, id uuid.UUID) error {
	panic("tilegen:hole orders.MemoryOrderStore.Delete")
}
