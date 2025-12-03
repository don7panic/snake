package examples

import (
	"context"
	"sync"
	"testing"

	"snake"

	"github.com/stretchr/testify/assert"
)

// CustomStore 是一个自定义的 Datastore 实现示例
// 展示如何使用 DatastoreFactory 注入自定义存储
type CustomStore struct {
	mu     sync.RWMutex
	data   map[string]any
	fieldA string
	fieldB string
}

func NewCustomStore(config string) *CustomStore {
	return &CustomStore{
		data: make(map[string]any),
	}
}

func (s *CustomStore) Set(taskID string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[taskID] = value
}

func (s *CustomStore) Get(taskID string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[taskID]
	return value, ok
}

// TestCustomDatastoreFactory 演示如何使用自定义 Datastore
func TestCustomDatastoreFactory(t *testing.T) {
	// 创建自定义工厂函数
	customConfig := "my-custom-config"
	factory := func() snake.Datastore {
		return NewCustomStore(customConfig)
	}

	// 使用自定义工厂创建 Engine
	engine := snake.NewEngine(
		snake.WithDatastoreFactory(factory),
	)

	data := &CustomStore{}
	// 注册任务
	task1 := snake.NewTask("task1", func(c context.Context, ctx *snake.Context) error {
		data.fieldA = "custom-value-1"
		return nil
	})

	task2 := snake.NewTask("task2", func(c context.Context, ctx *snake.Context) error {
		assert.Equal(t, "custom-value-1", data.fieldA)
		data.fieldB = "custom-value-2"
		return nil
	}, snake.WithDependsOn("task1"))

	assert.NoError(t, engine.Register(task1, task2))
	assert.NoError(t, engine.Build())

	// 执行
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// 验证结果
	assert.Equal(t, "custom-value-1", data.fieldA)
	assert.Equal(t, "custom-value-2", data.fieldB)
}

// TestMultipleExecutionsWithFactory 验证每次执行使用独立的 Datastore 实例
func TestMultipleExecutionsWithFactory(t *testing.T) {
	callCount := 0
	factory := func() snake.Datastore {
		callCount++
		return NewCustomStore("execution-" + string(rune('0'+callCount)))
	}

	engine := snake.NewEngine(
		snake.WithDatastoreFactory(factory),
	)

	task := snake.NewTask("task1", func(c context.Context, ctx *snake.Context) error {
		ctx.SetResult("task1", "result")
		return nil
	})

	assert.NoError(t, engine.Register(task))
	assert.NoError(t, engine.Build())

	// 第一次执行
	result1, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.True(t, result1.Success)

	// 第二次执行
	result2, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.True(t, result2.Success)

	// 验证工厂被调用了两次
	assert.Equal(t, 2, callCount)
}
