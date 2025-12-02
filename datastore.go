package snake

import "sync"

// Datastore defines the interface for thread-safe key-value storage
type Datastore interface {
	Init() Datastore
	Set(taskID string, value any)
	Get(taskID string) (value any, ok bool)
}

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

// Set stores a value in the datastore using the taskID as the key
// This method is thread-safe and uses a write lock
func (s *memoryMapStore) Set(taskID string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[taskID] = value
}

// Get retrieves a value from the datastore by taskID
// Returns the value and true if found, nil and false if not found
// This method is thread-safe and uses a read lock
func (s *memoryMapStore) Get(taskID string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[taskID]
	return value, ok
}

// Init returns a fresh datastore instance (or a reset version) for a new execution.
func (s *memoryMapStore) Init() Datastore {
	return newMemoryStore()
}
