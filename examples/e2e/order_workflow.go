package e2e

import (
	"context"
	"snake"
	"sync"
)

type OrderService struct {
	ctx context.Context
}

var (
	orderEngine     *snake.Engine
	orderEngineErr  error
	orderEngineOnce sync.Once
)

// NewOrderService is just a sample
func NewOrderService(ctx context.Context) *OrderService {
	return &OrderService{ctx: ctx}
}

type OrderRequest struct {
	UserID     string `json:"user_id"`
	SKUID      string `json:"skuid"`
	CouponCode string `json:"coupon_code"`
}

func (s *OrderService) validate(req *OrderRequest) error {
	// TODO
	return nil
}

func (s *OrderService) customerProfile(req *OrderRequest) error {
	// TODO
	return nil
}

func (s *OrderService) inventory(skuid string) error {
	// TODO
	return nil
}

func (s *OrderService) price(couponCode string) (float64, error) {
	// TODO
	return 0, nil
}

func (s *OrderService) charge() (float64, error) {
	// TODO
	return 0, nil
}

// The real business logic
func (s *OrderService) Pay(req *OrderRequest) error {
	orderEngineOnce.Do(func() {
		orderEngine = snake.NewEngine()
		validate := snake.NewTask("validate", func(c context.Context, ctx *snake.Context) error {
			if err := s.validate(req); err != nil {
				return err
			}
			return nil
		})

		customerProfile := snake.NewTask("customerProfile", func(c context.Context, ctx *snake.Context) error {
			if err := s.customerProfile(req); err != nil {
				return err
			}
			return nil
		}, snake.WithDependsOn("validate"))

		inventory := snake.NewTask("inventory", func(c context.Context, ctx *snake.Context) error {
			return nil
		}, snake.WithDependsOn("validate"))

		price := snake.NewTask("price", func(c context.Context, ctx *snake.Context) error {
			return nil
		}, snake.WithDependsOn("inventory", "customerProfile"))

		charge := snake.NewTask("charge", func(c context.Context, ctx *snake.Context) error {
			return nil
		}, snake.WithDependsOn("price"))
		if err := orderEngine.Register(
			validate,
			customerProfile,
			inventory,
			price,
			charge); err != nil {
			panic(err)
		}
		if err := orderEngine.Build(); err != nil {
			panic(err)
		}
	})
	result, err := orderEngine.Execute(s.ctx)
	if err != nil {
		return err
	}
	return nil
}
