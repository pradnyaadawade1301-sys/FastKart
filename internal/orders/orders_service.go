package order

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ─── Models ─────────────────────────────────────────────────

type OrderItem struct {
	MenuItemID     string  `json:"menu_item_id"`
	Quantity       int     `json:"quantity"`
	Customizations *string `json:"customizations,omitempty"`
}

type PlaceOrderRequest struct {
	RestaurantID        string      `json:"restaurant_id"`
	Items               []OrderItem `json:"items"`
	DeliveryAddress     string      `json:"delivery_address"`
	DeliveryLatitude    float64     `json:"delivery_latitude"`
	DeliveryLongitude   float64     `json:"delivery_longitude"`
	CouponCode          string      `json:"coupon_code,omitempty"`
	SpecialInstructions string      `json:"special_instructions,omitempty"`
	PaymentMethod       string      `json:"payment_method"` // razorpay, wallet, cod
}

type Order struct {
	ID                string           `json:"id"`
	UserID            string           `json:"user_id"`
	RestaurantID      string           `json:"restaurant_id"`
	RestaurantName    string           `json:"restaurant_name"`
	Status            string           `json:"status"`
	Items             []OrderItemDetail `json:"items"`
	DeliveryAddress   string           `json:"delivery_address"`
	Subtotal          float64          `json:"subtotal"`
	DeliveryFee       float64          `json:"delivery_fee"`
	Discount          float64          `json:"discount"`
	TotalAmount       float64          `json:"total_amount"`
	PaymentStatus     string           `json:"payment_status"`
	PaymentMethod     string           `json:"payment_method"`
	CouponCode        string           `json:"coupon_code,omitempty"`
	SpecialInstr      string           `json:"special_instructions,omitempty"`
	EstimatedDelivery *time.Time       `json:"estimated_delivery_time,omitempty"`
	DeliveredAt       *time.Time       `json:"delivered_at,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
}

type OrderItemDetail struct {
	ID         string  `json:"id"`
	MenuItemID string  `json:"menu_item_id"`
	ItemName   string  `json:"item_name"`
	ItemPrice  float64 `json:"item_price"`
	Quantity   int     `json:"quantity"`
	Subtotal   float64 `json:"subtotal"`
	ImageURL   string  `json:"image_url,omitempty"`
}

// ─── Service struct (tracking_ws.go ke saath share hota hai) ─

type Service struct {
	DB *sql.DB
}

// ─── Routes ─────────────────────────────────────────────────

func (s *Service) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/orders", s.PlaceOrder).Methods("POST")
	r.HandleFunc("/orders", s.GetUserOrders).Methods("GET")
	r.HandleFunc("/orders/{order_id}", s.GetOrderDetail).Methods("GET")
	r.HandleFunc("/orders/{order_id}/status", s.UpdateOrderStatus).Methods("PUT")
	r.HandleFunc("/orders/{order_id}/cancel", s.CancelOrder).Methods("POST")
	r.HandleFunc("/restaurant/{restaurant_id}/orders", s.GetRestaurantOrders).Methods("GET")
	// WebSocket route
	r.HandleFunc("/orders/track/ws/{order_id}", s.TrackWebSocket)
}

// ─── Track — current tracking data fetch karo ───────────────
// tracking_ws.go mein TrackWebSocket is method ko call karta hai

func (s *Service) Track(orderID string) (map[string]interface{}, error) {
	var status, driverName, driverPhone, driverImage sql.NullString
	var driverLat, driverLng sql.NullFloat64

	err := s.DB.QueryRow(`
		SELECT
			o.status,
			COALESCE(dp.name, '')        AS driver_name,
			COALESCE(dp.phone, '')       AS driver_phone,
			COALESCE(dp.profile_image, '') AS driver_image,
			dp.current_latitude,
			dp.current_longitude
		FROM orders o
		LEFT JOIN delivery_assignments da ON da.order_id = o.id AND da.status = 'active'
		LEFT JOIN delivery_partners dp ON dp.id = da.partner_id
		WHERE o.id = $1
	`, orderID).Scan(&status, &driverName, &driverPhone, &driverImage, &driverLat, &driverLng)

	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"status":       status.String,
		"driver_name":  driverName.String,
		"driver_phone": driverPhone.String,
		"driver_image": driverImage.String,
	}
	if driverLat.Valid {
		v := driverLat.Float64
		result["driver_lat"] = &v
	}
	if driverLng.Valid {
		v := driverLng.Float64
		result["driver_lng"] = &v
	}
	return result, nil
}

// ─── POST /orders ────────────────────────────────────────────

func (s *Service) PlaceOrder(rw http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)

	var req PlaceOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		jsonError(rw, "Order mein koi item nahi hai", http.StatusBadRequest)
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		jsonError(rw, "Transaction error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 1. Stock check
	for _, item := range req.Items {
		var stock int
		var itemName string
		err := tx.QueryRow(`
			SELECT COALESCE(i.stock_quantity, 999), mi.name
			FROM menu_items mi
			LEFT JOIN inventory i ON i.menu_item_id = mi.id AND i.restaurant_id = $1
			WHERE mi.id = $2 AND mi.is_available = true
		`, req.RestaurantID, item.MenuItemID).Scan(&stock, &itemName)

		if err == sql.ErrNoRows {
			jsonError(rw, fmt.Sprintf("Item %s available nahi hai", item.MenuItemID), http.StatusBadRequest)
			return
		}
		if stock < item.Quantity {
			jsonError(rw, fmt.Sprintf("'%s' ka stock kam hai. Available: %d", itemName, stock), http.StatusConflict)
			return
		}
	}

	// 2. Prices calculate
	type ItemWithPrice struct {
		MenuItemID string
		Name       string
		Price      float64
		Quantity   int
	}

	var subtotal float64
	var itemsWithPrices []ItemWithPrice

	for _, item := range req.Items {
		var iwp ItemWithPrice
		iwp.MenuItemID = item.MenuItemID
		iwp.Quantity = item.Quantity

		err := tx.QueryRow(`SELECT name, price FROM menu_items WHERE id = $1`,
			item.MenuItemID).Scan(&iwp.Name, &iwp.Price)
		if err != nil {
			jsonError(rw, "Item price fetch error", http.StatusInternalServerError)
			return
		}
		subtotal += iwp.Price * float64(item.Quantity)
		itemsWithPrices = append(itemsWithPrices, iwp)
	}

	// 3. Coupon apply
	var discount float64
	if req.CouponCode != "" {
		var discountType string
		var discountValue, minOrder float64
		var maxDiscount sql.NullFloat64

		err := tx.QueryRow(`
			SELECT discount_type, discount_value, min_order_value, max_discount
			FROM coupons
			WHERE code = $1 AND is_active = true
			  AND (valid_until IS NULL OR valid_until > NOW())
			  AND (usage_limit IS NULL OR used_count < usage_limit)
		`, req.CouponCode).Scan(&discountType, &discountValue, &minOrder, &maxDiscount)

		if err == nil && subtotal >= minOrder {
			if discountType == "percentage" {
				discount = subtotal * discountValue / 100
				if maxDiscount.Valid && discount > maxDiscount.Float64 {
					discount = maxDiscount.Float64
				}
			} else {
				discount = discountValue
			}
			tx.Exec(`UPDATE coupons SET used_count = used_count + 1 WHERE code = $1`, req.CouponCode)
		}
	}

	// 4. Delivery fee
	deliveryFee := 40.0
	if subtotal >= 300 {
		deliveryFee = 0
	}
	totalAmount := subtotal + deliveryFee - discount

	// 5. Order create
	orderID := uuid.New().String()
	estimatedDelivery := time.Now().Add(40 * time.Minute)

	_, err = tx.Exec(`
		INSERT INTO orders (
			id, user_id, restaurant_id, status,
			delivery_address, delivery_latitude, delivery_longitude,
			subtotal, delivery_fee, discount, total_amount,
			payment_status, payment_method,
			coupon_code, special_instructions, estimated_delivery_time
		) VALUES ($1,$2,$3,'pending',$4,$5,$6,$7,$8,$9,$10,'pending',$11,$12,$13,$14)`,
		orderID, userID, req.RestaurantID,
		req.DeliveryAddress, req.DeliveryLatitude, req.DeliveryLongitude,
		subtotal, deliveryFee, discount, totalAmount,
		req.PaymentMethod, req.CouponCode, req.SpecialInstructions, estimatedDelivery,
	)
	if err != nil {
		jsonError(rw, "Order create error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 6. Order items insert
	for _, item := range itemsWithPrices {
		itemSubtotal := item.Price * float64(item.Quantity)
		_, err = tx.Exec(`
			INSERT INTO order_items (id, order_id, menu_item_id, item_name, item_price, quantity, subtotal)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			uuid.New().String(), orderID, item.MenuItemID,
			item.Name, item.Price, item.Quantity, itemSubtotal)
		if err != nil {
			jsonError(rw, "Order item insert error", http.StatusInternalServerError)
			return
		}
	}

	// 7. Status history
	tx.Exec(`
		INSERT INTO order_status_history (id, order_id, status, note)
		VALUES ($1,$2,'pending','Order placed by customer')`, uuid.New().String(), orderID)

	// 8. COD/Wallet → turant confirm
	if req.PaymentMethod == "cod" || req.PaymentMethod == "wallet" {
		tx.Exec(`UPDATE orders SET status = 'confirmed' WHERE id = $1`, orderID)
	}

	tx.Commit()

	// WebSocket broadcast
	status := "confirmed"
	if req.PaymentMethod == "razorpay" {
		status = "pending_payment"
	}
	GlobalHub.BroadcastStatus(orderID, TrackingPayload{
		OrderID: orderID,
		Status:  status,
		Message: "Order place ho gaya!",
	})

	jsonResp(rw, map[string]interface{}{
		"message":            "Order successfully place ho gaya!",
		"order_id":           orderID,
		"total":              totalAmount,
		"subtotal":           subtotal,
		"delivery_fee":       deliveryFee,
		"discount":           discount,
		"estimated_delivery": estimatedDelivery,
		"payment_method":     req.PaymentMethod,
		"status":             status,
	})
}

// ─── GET /orders ─────────────────────────────────────────────

func (s *Service) GetUserOrders(rw http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)

	rows, err := s.DB.Query(`
		SELECT o.id, o.restaurant_id, r.name,
			o.status, o.total_amount, o.payment_status,
			o.payment_method, o.created_at,
			COUNT(oi.id) AS item_count
		FROM orders o
		JOIN restaurants r ON r.id = o.restaurant_id
		LEFT JOIN order_items oi ON oi.order_id = o.id
		WHERE o.user_id = $1
		GROUP BY o.id, r.name
		ORDER BY o.created_at DESC
	`, userID)
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type OrderSummary struct {
		ID             string    `json:"id"`
		RestaurantID   string    `json:"restaurant_id"`
		RestaurantName string    `json:"restaurant_name"`
		Status         string    `json:"status"`
		TotalAmount    float64   `json:"total_amount"`
		PaymentStatus  string    `json:"payment_status"`
		PaymentMethod  string    `json:"payment_method"`
		ItemCount      int       `json:"item_count"`
		CreatedAt      time.Time `json:"created_at"`
	}

	var orders []OrderSummary
	for rows.Next() {
		var o OrderSummary
		rows.Scan(&o.ID, &o.RestaurantID, &o.RestaurantName,
			&o.Status, &o.TotalAmount, &o.PaymentStatus,
			&o.PaymentMethod, &o.CreatedAt, &o.ItemCount)
		orders = append(orders, o)
	}

	jsonResp(rw, map[string]interface{}{"orders": orders, "count": len(orders)})
}

// ─── GET /orders/{order_id} ──────────────────────────────────

func (s *Service) GetOrderDetail(rw http.ResponseWriter, r *http.Request) {
	orderID := mux.Vars(r)["order_id"]
	userID := r.Context().Value("user_id").(string)

	var order Order
	err := s.DB.QueryRow(`
		SELECT o.id, o.user_id, o.restaurant_id, r.name,
			o.status, o.delivery_address,
			o.subtotal, o.delivery_fee, o.discount, o.total_amount,
			o.payment_status, o.payment_method,
			COALESCE(o.coupon_code,''), COALESCE(o.special_instructions,''),
			o.estimated_delivery_time, o.delivered_at, o.created_at
		FROM orders o
		JOIN restaurants r ON r.id = o.restaurant_id
		WHERE o.id = $1 AND (o.user_id = $2 OR $2 = 'admin')
	`, orderID, userID).Scan(
		&order.ID, &order.UserID, &order.RestaurantID, &order.RestaurantName,
		&order.Status, &order.DeliveryAddress,
		&order.Subtotal, &order.DeliveryFee, &order.Discount, &order.TotalAmount,
		&order.PaymentStatus, &order.PaymentMethod,
		&order.CouponCode, &order.SpecialInstr,
		&order.EstimatedDelivery, &order.DeliveredAt, &order.CreatedAt,
	)
	if err == sql.ErrNoRows {
		jsonError(rw, "Order nahi mila", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}

	rows, err := s.DB.Query(`
		SELECT id, menu_item_id, item_name, item_price, quantity, subtotal
		FROM order_items WHERE order_id = $1`, orderID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item OrderItemDetail
			rows.Scan(&item.ID, &item.MenuItemID, &item.ItemName,
				&item.ItemPrice, &item.Quantity, &item.Subtotal)
			order.Items = append(order.Items, item)
		}
	}

	jsonResp(rw, order)
}

// ─── PUT /orders/{order_id}/status ──────────────────────────

func (s *Service) UpdateOrderStatus(rw http.ResponseWriter, r *http.Request) {
	orderID := mux.Vars(r)["order_id"]

	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "Invalid request", http.StatusBadRequest)
		return
	}

	validStatuses := map[string]bool{
		"confirmed": true, "preparing": true, "ready": true,
		"picked_up": true, "delivered": true, "cancelled": true,
	}
	if !validStatuses[req.Status] {
		jsonError(rw, "Invalid status", http.StatusBadRequest)
		return
	}

	tx, _ := s.DB.Begin()
	defer tx.Rollback()

	query := `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`
	if req.Status == "delivered" {
		query = `UPDATE orders SET status = $1, delivered_at = NOW(), updated_at = NOW() WHERE id = $2`
	}
	if _, err := tx.Exec(query, req.Status, orderID); err != nil {
		jsonError(rw, "Status update error", http.StatusInternalServerError)
		return
	}

	updaterID := r.Context().Value("user_id").(string)
	tx.Exec(`
		INSERT INTO order_status_history (id, order_id, status, note, updated_by)
		VALUES ($1,$2,$3,$4,$5)`,
		uuid.New().String(), orderID, req.Status, req.Note, updaterID)

	tx.Commit()

	// WebSocket broadcast
	GlobalHub.BroadcastStatus(orderID, TrackingPayload{
		OrderID: orderID,
		Status:  req.Status,
		Message: req.Note,
	})

	jsonResp(rw, map[string]interface{}{
		"message":  "Status update ho gaya",
		"status":   req.Status,
		"order_id": orderID,
	})
}

// ─── POST /orders/{order_id}/cancel ─────────────────────────

func (s *Service) CancelOrder(rw http.ResponseWriter, r *http.Request) {
	orderID := mux.Vars(r)["order_id"]
	userID := r.Context().Value("user_id").(string)

	var currentStatus string
	err := s.DB.QueryRow(`
		SELECT status FROM orders WHERE id = $1 AND user_id = $2`,
		orderID, userID).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		jsonError(rw, "Order nahi mila", http.StatusNotFound)
		return
	}

	if currentStatus == "preparing" || currentStatus == "picked_up" || currentStatus == "delivered" {
		jsonError(rw, fmt.Sprintf("'%s' status mein cancel nahi ho sakta", currentStatus), http.StatusConflict)
		return
	}

	tx, _ := s.DB.Begin()
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE orders SET status='cancelled', updated_at=NOW() WHERE id=$1`, orderID); err != nil {
		jsonError(rw, "Cancel error", http.StatusInternalServerError)
		return
	}

	tx.Exec(`
		INSERT INTO order_status_history (id, order_id, status, note, updated_by)
		VALUES ($1,$2,'cancelled','Cancelled by customer',$3)`,
		uuid.New().String(), orderID, userID)

	tx.Commit()

	GlobalHub.BroadcastStatus(orderID, TrackingPayload{
		OrderID: orderID,
		Status:  "cancelled",
		Message: "Order cancel ho gaya",
	})

	jsonResp(rw, map[string]interface{}{
		"message":  "Order cancel ho gaya",
		"order_id": orderID,
	})
}

// ─── GET /restaurant/{restaurant_id}/orders ─────────────────

func (s *Service) GetRestaurantOrders(rw http.ResponseWriter, r *http.Request) {
	restaurantID := mux.Vars(r)["restaurant_id"]
	status := r.URL.Query().Get("status")

	query := `
		SELECT o.id, o.user_id, u.name,
			o.status, o.total_amount, o.payment_method,
			o.special_instructions, o.created_at
		FROM orders o
		JOIN users u ON u.id = o.user_id
		WHERE o.restaurant_id = $1`

	args := []interface{}{restaurantID}
	if status != "" {
		query += " AND o.status = $2 ORDER BY o.created_at DESC"
		args = append(args, status)
	} else {
		query += " ORDER BY o.created_at DESC LIMIT 100"
	}

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RestaurantOrder struct {
		ID            string    `json:"id"`
		UserID        string    `json:"user_id"`
		CustomerName  string    `json:"customer_name"`
		Status        string    `json:"status"`
		TotalAmount   float64   `json:"total_amount"`
		PaymentMethod string    `json:"payment_method"`
		SpecialInstr  string    `json:"special_instructions"`
		CreatedAt     time.Time `json:"created_at"`
	}

	var orders []RestaurantOrder
	for rows.Next() {
		var o RestaurantOrder
		rows.Scan(&o.ID, &o.UserID, &o.CustomerName,
			&o.Status, &o.TotalAmount, &o.PaymentMethod,
			&o.SpecialInstr, &o.CreatedAt)
		orders = append(orders, o)
	}

	jsonResp(rw, map[string]interface{}{"orders": orders, "count": len(orders)})
}

// ─── Helpers ─────────────────────────────────────────────────

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}