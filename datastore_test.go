package snake

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMapStore(t *testing.T) {
	store := newMemoryStore()
	assert.NotNil(t, store)
	assert.NotNil(t, store.data)
}

func TestMapStore_SetAndGet(t *testing.T) {
	store := newMemoryStore()

	// Test setting and getting a value
	store.Set("task1", "value1")
	value, ok := store.Get("task1")
	assert.True(t, ok)
	assert.Equal(t, "value1", value)

	// Test getting a non-existent key
	value, ok = store.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, value)
}

func TestMapStore_SetOverwrite(t *testing.T) {
	store := newMemoryStore()

	// Set initial value
	store.Set("task1", "value1")
	value, ok := store.Get("task1")
	assert.True(t, ok)
	assert.Equal(t, "value1", value)

	// Overwrite with new value
	store.Set("task1", "value2")
	value, ok = store.Get("task1")
	assert.True(t, ok)
	assert.Equal(t, "value2", value)
}

func TestMapStore_MultipleKeys(t *testing.T) {
	store := newMemoryStore()

	// Set multiple values
	store.Set("task1", "value1")
	store.Set("task2", 42)
	store.Set("task3", []string{"a", "b", "c"})

	// Verify all values
	value1, ok1 := store.Get("task1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", value1)

	value2, ok2 := store.Get("task2")
	assert.True(t, ok2)
	assert.Equal(t, 42, value2)

	value3, ok3 := store.Get("task3")
	assert.True(t, ok3)
	assert.Equal(t, []string{"a", "b", "c"}, value3)
}

func TestMapStore_ConcurrentAccess(t *testing.T) {
	store := newMemoryStore()
	var wg sync.WaitGroup

	// Number of concurrent operations
	numWriters := 10
	numReaders := 10
	numOperations := 100

	// Concurrent writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				taskID := "task" + string(rune(writerID))
				store.Set(taskID, writerID*numOperations+j)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				taskID := "task" + string(rune(readerID%numWriters))
				store.Get(taskID)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify that we can still read values after concurrent access
	for i := 0; i < numWriters; i++ {
		taskID := "task" + string(rune(i))
		_, ok := store.Get(taskID)
		assert.True(t, ok, "Expected task %s to exist after concurrent writes", taskID)
	}
}

func TestMapStore_NilValue(t *testing.T) {
	store := newMemoryStore()

	// Test storing nil value
	store.Set("task1", nil)
	value, ok := store.Get("task1")
	assert.True(t, ok, "Key should exist even with nil value")
	assert.Nil(t, value)
}

func TestMapStore_EmptyKey(t *testing.T) {
	store := newMemoryStore()

	// Test with empty string key
	store.Set("", "value")
	value, ok := store.Get("")
	assert.True(t, ok)
	assert.Equal(t, "value", value)
}
