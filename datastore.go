package snake

import "sync"

// Datastore defines the interface for thread-safe key-value storage
type Datastore interface {
	Set(key string, value any)
	Get(key string) (value any, ok bool)
}

// DatastoreFactory is a function that creates new Datastore instances
// Each execution will use a fresh instance created by this factory
type DatastoreFactory func() Datastore

// memoryMapStore is a thread-safe in-memory implementation of the Datastore interface
type memoryMapStore struct {
	mu   sync.RWMutex
	data map[string]any
}

// newMemoryStore creates a new empty MapStore instance
func newMemoryStore() *memoryMapStore {
	return &memoryMapStore{
		data: make(map[string]any),
	}
}

// Set stores a value in the datastore using the key as the key
// This method is thread-safe and uses a write lock
func (s *memoryMapStore) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// Get retrieves a value from the datastore by key
// Returns the value and true if found, nil and false if not found
// This method is thread-safe and uses a read lock
func (s *memoryMapStore) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[key]
	return value, ok
}
