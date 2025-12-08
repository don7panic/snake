package examples

import (
	"context"
	"sync"
	"testing"

	"github.com/don7panic/snake"

	"github.com/stretchr/testify/assert"
)

// StronglyTypedStore 展示带有固定字段（含结构体指针）的自定义存储
// 通过锁保证并发安全，同时返回拷贝避免调用方意外修改内部状态。
type StronglyTypedStore struct {
	mu     sync.RWMutex
	user   *User
	status string
}

type User struct {
	Name string
	Age  int
}

// Set implements snake.Datastore for StronglyTypedStore.
func (s *StronglyTypedStore) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch key {
	case "user":
		if value == nil {
			s.user = nil
			return
		}
		u := value.(*User)
		copied := *u // 防止外部修改同一实例
		s.user = &copied
	case "status":
		s.status = value.(string)
	default:
		// 为了示例简单，未知 key 直接忽略或 panic 均可，这里选择忽略。
	}
}

// Get implements snake.Datastore for StronglyTypedStore.
func (s *StronglyTypedStore) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch key {
	case "user":
		if s.user == nil {
			return nil, false
		}
		copied := *s.user
		return &copied, true
	case "status":
		if s.status == "" {
			return nil, false
		}
		return s.status, true
	default:
		return nil, false
	}
}

// TestStronglyTypedStore 演示自定义存储包含结构体字段的用法
func TestStronglyTypedStore(t *testing.T) {
	factory := func() snake.Datastore {
		return &StronglyTypedStore{}
	}

	engine := snake.NewEngine(
		snake.WithDatastoreFactory(factory),
	)

	task1 := snake.NewTask("task1", func(c context.Context, ctx *snake.Context) error {
		ctx.SetResult("user", &User{Name: "alice", Age: 20})
		ctx.SetResult("status", "initialized")
		return nil
	})

	task2 := snake.NewTask("task2", func(c context.Context, ctx *snake.Context) error {
		raw, ok := ctx.GetResult("user")
		assert.True(t, ok)
		user := raw.(*User)

		// 更新用户信息并写回，内部会复制一份，避免共享实例被外部改动
		ctx.SetResult("user", &User{Name: user.Name, Age: user.Age + 1})
		ctx.SetResult("status", "updated")
		return nil
	}, snake.WithDependsOn("task1"))

	task3 := snake.NewTask("task3", func(c context.Context, ctx *snake.Context) error {
		rawUser, ok := ctx.GetResult("user")
		assert.True(t, ok)
		user := rawUser.(*User)
		assert.Equal(t, "alice", user.Name)
		assert.Equal(t, 21, user.Age)

		rawStatus, ok := ctx.GetResult("status")
		assert.True(t, ok)
		assert.Equal(t, "updated", rawStatus)
		return nil
	}, snake.WithDependsOn("task2"))

	assert.NoError(t, engine.Register(task1, task2, task3))
	assert.NoError(t, engine.Build())

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// Verify datastore retained the latest values
	uVal, ok := result.GetResult("user")
	assert.True(t, ok)
	user := uVal.(*User)
	assert.Equal(t, "alice", user.Name)
	assert.Equal(t, 21, user.Age)

	status, ok := result.GetResult("status")
	assert.True(t, ok)
	assert.Equal(t, "updated", status)
}
