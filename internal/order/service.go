// internal/order/service.go
package order

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/honor/fastkart-backend/pkg/database"
)

type Service struct{}

func NewService() *Service { return &Service{} }

type PlaceOrderRequest struct {
	UserID          string      `json:"-"`
	RestaurantID    string      `json:"restaurant_id" binding:"required"`
	RestaurantName  string      `json:"restaurant_name"`
	RestaurantImage string      `json:"restaurant_image"`
	Items           []OrderItem `json:"items" binding:"required"`
	Subtotal        float64     `json:"subtotal"`
	DeliveryFee     float64     `json:"delivery_fee"`
	Discount        float64     `json:"discount"`
	Total           float64     `json:"total"`
	DeliveryAddress Address     `json:"delivery_address" binding:"required"`
	PaymentMethod   string      `json:"payment_method"`
	CouponCode      string      `json:"coupon_code"`
}

type OrderItem struct {
	FoodID   string  `json:"food_id"`
	Name     string  `json:"name"`
	ImageURL string  `json:"image_url"`
	Price    float64 `json:"price"`
	Quantity int     `json:"quantity"`
	Total    float64 `json:"total"`
}

type Address struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Line1       string `json:"line1"`
	City        string `json:"city"`
	Pincode     string `json:"pincode"`
	FullAddress string `json:"full_address"`
}

func (s *Service) Place(req PlaceOrderRequest) (map[string]interface{}, error) {
	itemsJSON, _ := json.Marshal(req.Items)
	addrJSON, _ := json.Marshal(req.DeliveryAddress)
	otp := fmt.Sprintf("%04d", time.Now().UnixNano()%9000+1000)
	estimated := time.Now().Add(35 * time.Minute)

	// ── Order insert karo ──────────────────────────────────────────────────
	var orderID string
	err := database.DB.QueryRow(`
		INSERT INTO orders (
			user_id, restaurant_id, restaurant_name, restaurant_image,
			items, subtotal, delivery_fee, discount, total,
			delivery_address, payment_method, coupon_code,
			status, otp, estimated_delivery, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'placed',$13,$14,NOW())
		RETURNING id`,
		req.UserID, req.RestaurantID, req.RestaurantName, req.RestaurantImage,
		itemsJSON, req.Subtotal, req.DeliveryFee, req.Discount, req.Total,
		addrJSON, req.PaymentMethod, req.CouponCode, otp, estimated,
	).Scan(&orderID)
	if err != nil {
		return nil, err
	}

	// ── Nearest warehouse find karo aur log banao ──────────────────────────
	var warehouseID string
	database.DB.QueryRow(
		`SELECT id FROM warehouse WHERE is_active = true LIMIT 1`,
	).Scan(&warehouseID)

	if warehouseID != "" {
		database.DB.Exec(`
			INSERT INTO order_warehouse_log (order_id, warehouse_id, status)
			VALUES ($1, $2, 'received')`,
			orderID, warehouseID)
	}

	// ── Notification banao ────────────────────────────────────────────────
	database.DB.Exec(`
		INSERT INTO notifications (user_id, title, body, type, ref_id)
		VALUES ($1, $2, $3, 'order_update', $4)`,
		req.UserID,
		"Order Placed! 🎉",
		fmt.Sprintf("Your order from %s has been placed successfully.", req.RestaurantName),
		orderID,
	)

	return map[string]interface{}{
		"id":                 orderID,
		"status":             "placed",
		"otp":                otp,
		"estimated_delivery": estimated,
		"total":              req.Total,
		"warehouse_status":   "received",
	}, nil
}

func (s *Service) ListByUser(userID, status string) ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT o.id, o.restaurant_name, o.restaurant_image, o.total,
		       o.status, o.created_at, o.estimated_delivery,
		       COALESCE(owl.status, 'received') AS warehouse_status
		FROM orders o
		LEFT JOIN order_warehouse_log owl ON owl.order_id = o.id
		WHERE o.user_id = $1 AND ($2 = '' OR o.status = $2)
		ORDER BY o.created_at DESC`,
		userID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []map[string]interface{}
	for rows.Next() {
		var id, name, img, status, warehouseStatus string
		var total float64
		var createdAt time.Time
		var estimatedAt *time.Time
		if err := rows.Scan(&id, &name, &img, &total, &status,
			&createdAt, &estimatedAt, &warehouseStatus); err != nil {
			continue
		}
		orders = append(orders, map[string]interface{}{
			"id": id, "restaurant_name": name, "restaurant_image": img,
			"total": total, "status": status, "created_at": createdAt,
			"estimated_delivery": estimatedAt,
			"warehouse_status":   warehouseStatus,
		})
	}
	return orders, nil
}

func (s *Service) GetByID(id, userID string) (map[string]interface{}, error) {
	var restaurantName, img, status, paymentMethod, itemsRaw, addrRaw, otp string
	var subtotal, deliveryFee, discount, total float64
	var createdAt time.Time
	var estimatedAt *time.Time

	err := database.DB.QueryRow(`
		SELECT restaurant_name, restaurant_image, status, payment_method,
		       items, delivery_address, subtotal, delivery_fee,
		       discount, total, otp, created_at, estimated_delivery
		FROM orders WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&restaurantName, &img, &status, &paymentMethod,
		&itemsRaw, &addrRaw, &subtotal, &deliveryFee,
		&discount, &total, &otp, &createdAt, &estimatedAt)
	if err != nil {
		return nil, err
	}

	var items []interface{}
	var addr interface{}
	json.Unmarshal([]byte(itemsRaw), &items)
	json.Unmarshal([]byte(addrRaw), &addr)

	// Warehouse status bhi fetch karo
	var warehouseStatus, warehouseName string
	database.DB.QueryRow(`
		SELECT COALESCE(owl.status,'received'), COALESCE(w.name,'Processing')
		FROM order_warehouse_log owl
		LEFT JOIN warehouse w ON w.id = owl.warehouse_id
		WHERE owl.order_id = $1`, id,
	).Scan(&warehouseStatus, &warehouseName)

	return map[string]interface{}{
		"id": id, "restaurant_name": restaurantName,
		"restaurant_image": img, "status": status,
		"payment_method": paymentMethod, "items": items,
		"delivery_address": addr, "subtotal": subtotal,
		"delivery_fee": deliveryFee, "discount": discount,
		"total": total, "otp": otp,
		"created_at": createdAt, "estimated_delivery": estimatedAt,
		"warehouse_status": warehouseStatus,
		"warehouse_name":   warehouseName,
	}, nil
}

func (s *Service) Cancel(id, userID string) error {
	var status string
	err := database.DB.QueryRow(
		`SELECT status FROM orders WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&status)
	if err != nil {
		return errors.New("order not found")
	}
	if status == "delivered" || status == "cancelled" {
		return errors.New("order cannot be cancelled")
	}
	_, err = database.DB.Exec(
		`UPDATE orders SET status = 'cancelled' WHERE id = $1`, id)

	// Warehouse log bhi update karo
	database.DB.Exec(
		`UPDATE order_warehouse_log SET status='cancelled' WHERE order_id=$1`, id)
	return err
}

func (s *Service) Track(id string) (map[string]interface{}, error) {
	var status, driverName, driverPhone, driverImage string
	var driverLat, driverLng *float64
	var estimatedAt *time.Time

	err := database.DB.QueryRow(`
		SELECT o.status,
		       COALESCE(d.name,''), COALESCE(d.phone,''), COALESCE(d.image_url,''),
		       d.current_lat, d.current_lng, o.estimated_delivery
		FROM orders o
		LEFT JOIN drivers d ON d.id = o.driver_id
		WHERE o.id = $1`, id,
	).Scan(&status, &driverName, &driverPhone, &driverImage,
		&driverLat, &driverLng, &estimatedAt)
	if err != nil {
		return nil, err
	}

	// Warehouse timeline bhi add karo
	var warehouseStatus string
	var pickedAt, packedAt, dispatchedAt *time.Time
	database.DB.QueryRow(`
		SELECT COALESCE(status,'received'), picked_at, packed_at, dispatched_at
		FROM order_warehouse_log WHERE order_id = $1`, id,
	).Scan(&warehouseStatus, &pickedAt, &packedAt, &dispatchedAt)

	return map[string]interface{}{
		"order_id": id, "status": status,
		"driver_name": driverName, "driver_phone": driverPhone,
		"driver_image":       driverImage,
		"driver_lat":         driverLat,
		"driver_lng":         driverLng,
		"estimated_delivery": estimatedAt,
		"warehouse_status":   warehouseStatus,
		"picked_at":          pickedAt,
		"packed_at":          packedAt,
		"dispatched_at":      dispatchedAt,
	}, nil
}