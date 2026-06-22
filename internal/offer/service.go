// internal/offer/service.go
package offer

import (
	"errors"
	"fmt"
	"time"

	"github.com/honor/fastkart-backend/pkg/database"
	redisclient "github.com/honor/fastkart-backend/pkg/redis"
)

type Service struct{}

func NewService() *Service { return &Service{} }

type Offer struct {
	ID            string    `json:"id"`
	Code          string    `json:"code"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	DiscountType  string    `json:"discount_type"`
	DiscountValue float64   `json:"discount_value"`
	MinOrder      float64   `json:"min_order"`
	MaxDiscount   float64   `json:"max_discount"`
	ExpiresAt     time.Time `json:"expires_at"`
	IsActive      bool      `json:"is_active"`
}

type ApplyResult struct {
	Code           string  `json:"code"`
	DiscountType   string  `json:"discount_type"`
	DiscountValue  float64 `json:"discount_value"`
	DiscountAmount float64 `json:"discount_amount"`
	FinalTotal     float64 `json:"final_total"`
}

// List — saare active offers lo (Redis cache 5 min)
func (s *Service) List() ([]Offer, error) {
	cacheKey := "offers:active"
	if redisclient.IsAvailable() {
		if _, ok := redisclient.Get(cacheKey); ok {
			// Production mein JSON unmarshal karke return karo
		}
	}

	rows, err := database.DB.Query(`
		SELECT id, code, title, description, discount_type, discount_value,
		       min_order, max_discount, expires_at, is_active
		FROM offers
		WHERE is_active = true AND expires_at > NOW()
		ORDER BY created_at DESC
		LIMIT 20`)
	if err != nil {
		return []Offer{}, nil
	}
	defer rows.Close()

	var offers []Offer
	for rows.Next() {
		var o Offer
		if err := rows.Scan(&o.ID, &o.Code, &o.Title, &o.Description,
			&o.DiscountType, &o.DiscountValue, &o.MinOrder,
			&o.MaxDiscount, &o.ExpiresAt, &o.IsActive); err == nil {
			offers = append(offers, o)
		}
	}
	if offers == nil {
		offers = []Offer{}
	}

	if redisclient.IsAvailable() {
		redisclient.Set(cacheKey, "cached", 5*60*1000000000)
	}
	return offers, nil
}

// Apply — coupon validate karo aur discount calculate karo
func (s *Service) Apply(code string, cartTotal float64) (*ApplyResult, error) {
	var o Offer
	err := database.DB.QueryRow(`
		SELECT id, code, discount_type, discount_value, min_order, max_discount
		FROM offers
		WHERE UPPER(code) = UPPER($1)
		  AND is_active = true
		  AND expires_at > NOW()`, code).Scan(
		&o.ID, &o.Code, &o.DiscountType, &o.DiscountValue, &o.MinOrder, &o.MaxDiscount)
	if err != nil {
		return nil, errors.New("invalid ya expired coupon code")
	}

	if cartTotal < o.MinOrder {
		return nil, fmt.Errorf("minimum order ₹%.0f chahiye", o.MinOrder)
	}

	var discountAmount float64
	if o.DiscountType == "percent" {
		discountAmount = cartTotal * o.DiscountValue / 100
		if o.MaxDiscount > 0 && discountAmount > o.MaxDiscount {
			discountAmount = o.MaxDiscount
		}
	} else {
		discountAmount = o.DiscountValue
	}

	if discountAmount > cartTotal {
                discountAmount = cartTotal
        }  // ← add this closing brace

        

	return &ApplyResult{
		Code:           o.Code,
		DiscountType:   o.DiscountType,
		DiscountValue:  o.DiscountValue,
		DiscountAmount: discountAmount,
		FinalTotal:     cartTotal - discountAmount,
	}, nil
}