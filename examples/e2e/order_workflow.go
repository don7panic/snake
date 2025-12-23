package e2e

import (
	"context"
	"fmt"
	"sync"

	"github.com/don7panic/snake"
)

type OrderService struct {
	ctx context.Context
}

var (
	orderEngine     *snake.Engine
	orderEngineOnce sync.Once
)

// NewOrderService is just a sample
func NewOrderService(ctx context.Context) *OrderService {
	return &OrderService{
		ctx: ctx,
	}
}

type OrderRequest struct {
	UserID     string `json:"user_id"`
	SKUID      string `json:"skuid"`
	CouponCode string `json:"coupon_code"`
}

func (s *OrderService) validate(req *OrderRequest) error {
	if req == nil {
		return fmt.Errorf("nil request")
	}
	if req.UserID == "" {
		return fmt.Errorf("missing user_id")
	}
	if req.SKUID == "" {
		return fmt.Errorf("missing skuid")
	}

	blockedUsers := map[string]bool{
		"blocked-user": true,
	}
	if blockedUsers[req.UserID] {
		return fmt.Errorf("user %s is blocked", req.UserID)
	}
	return nil
}

func (s *OrderService) customerProfile(req *OrderRequest) error {
	if req == nil {
		return fmt.Errorf("nil request")
	}
	// Simulate a profile lookup; here we just allow all non-blocked users.
	return nil
}

func (s *OrderService) inventory(skuid string) error {
	if skuid == "" {
		return fmt.Errorf("missing skuid")
	}

	stocks := map[string]int{
		"sku-1": 10,
		"sku-2": 5,
	}
	stock, ok := stocks[skuid]
	if !ok {
		return fmt.Errorf("unknown sku %s", skuid)
	}
	if stock <= 0 {
		return fmt.Errorf("sku %s out of stock", skuid)
	}
	return nil
}

func (s *OrderService) price(skuid, couponCode string) (float64, error) {
	if skuid == "" {
		return 0, fmt.Errorf("missing skuid")
	}

	prices := map[string]float64{
		"sku-1": 100,
		"sku-2": 199,
	}
	base, ok := prices[skuid]
	couponDiscount := map[string]float64{
		"NEW10": 0.10,
		"VIP20": 0.20,
	}
	discount := couponDiscount[couponCode]

	if !ok {
		return 0, fmt.Errorf("unknown sku %s", skuid)
	}

	final := base * (1 - discount)
	if final < 0 {
		final = 0
	}
	return final, nil
}

func (s *OrderService) charge(amount float64) (float64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("invalid charge amount: %.2f", amount)
	}
	// In real life this would call a payment provider; here we simply echo back.
	return amount, nil
}

// The real business logic
func (s *OrderService) Pay(req *OrderRequest) error {
	orderEngineOnce.Do(func() {
		orderEngine = snake.NewEngine()
		validate := snake.NewTask("validate", func(c context.Context, ctx *snake.Context) error {
			// Get input from Context instead of closure capture
			input, ok := ctx.Input().(*OrderRequest)
			if !ok {
				return nil // Handle nil input gracefully
			}
			if err := s.validate(input); err != nil {
				return err
			}
			ctx.SetResult("validate", "ok")
			return nil
		})

		customerProfile := snake.NewTask("customerProfile", func(c context.Context, ctx *snake.Context) error {
			// Get input from Context instead of closure capture
			input, ok := ctx.Input().(*OrderRequest)
			if !ok {
				return nil // Handle nil input gracefully
			}
			if err := s.customerProfile(input); err != nil {
				return err
			}
			ctx.SetResult("customerProfile", "ok")
			return nil
		}, snake.WithDependsOn(validate))

		inventory := snake.NewTask("inventory", func(c context.Context, ctx *snake.Context) error {
			// Get input from Context instead of closure capture
			input, ok := ctx.Input().(*OrderRequest)
			if !ok {
				return nil // Handle nil input gracefully
			}
			if err := s.inventory(input.SKUID); err != nil {
				return err
			}
			ctx.SetResult("inventory", "reserved")
			return nil
		}, snake.WithDependsOn(validate))

		price := snake.NewTask("price", func(c context.Context, ctx *snake.Context) error {
			// Get input from Context instead of closure capture
			input, ok := ctx.Input().(*OrderRequest)
			if !ok {
				return nil // Handle nil input gracefully
			}
			priceVal, err := s.price(input.SKUID, input.CouponCode)
			if err != nil {
				return err
			}
			ctx.SetResult("price", priceVal)
			return nil
		}, snake.WithDependsOn(inventory, customerProfile))

		charge := snake.NewTask("charge", func(c context.Context, ctx *snake.Context) error {
			priceVal, ok := ctx.GetResult("price")
			if !ok {
				return fmt.Errorf("price missing")
			}
			priceFloat, ok := priceVal.(float64)
			if !ok {
				return fmt.Errorf("price has unexpected type %T", priceVal)
			}

			chargeVal, err := s.charge(priceFloat)
			if err != nil {
				return err
			}
			ctx.SetResult("charge", chargeVal)
			return nil
		}, snake.WithDependsOn(price))
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
	_, err := orderEngine.Execute(s.ctx, req)
	if err != nil {
		return err
	}
	return nil
}
