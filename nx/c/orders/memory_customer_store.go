// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package orders

import (
	"context"
	"sync"
)

// MemoryCustomerStore implements CustomerStore using memory storage.
type MemoryCustomerStore struct {
	mu sync.Mutex
	m  map[string]*Customer
}

// NewMemoryCustomerStore returns a ready-to-use MemoryCustomerStore.
func NewMemoryCustomerStore() *MemoryCustomerStore {
	return &MemoryCustomerStore{m: make(map[string]*Customer)}
}

func (s *MemoryCustomerStore) Get(ctx context.Context, id string) (*Customer, error) {
	panic("tilegen:hole orders.MemoryCustomerStore.Get")
}

func (s *MemoryCustomerStore) Save(ctx context.Context, customer *Customer) error {
	panic("tilegen:hole orders.MemoryCustomerStore.Save")
}
