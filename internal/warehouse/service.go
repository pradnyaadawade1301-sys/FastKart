// internal/warehouse/service.go
package warehouse

import (
	"time"

	"github.com/honor/fastkart-backend/internal/order" // ✅ add
	"github.com/honor/fastkart-backend/pkg/database"
)

type Service struct{}

func NewService() *Service { return &Service{} }

// ListOrders — saare orders with warehouse log
func (s *Service) ListOrders(status, warehouseID string) ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT
			o.id, o.restaurant_name, o.total, o.status AS order_status,
			o.created_at, o.delivery_address,
			COALESCE(owl.status, 'pending') AS warehouse_status,
			COALESCE(w.name, 'Unassigned') AS warehouse_name,
			owl.picked_at, owl.packed_at, owl.dispatched_at, owl.delivered_at
		FROM orders o
		LEFT JOIN order_warehouse_log owl ON owl.order_id = o.id
		LEFT JOIN warehouse w ON w.id = owl.warehouse_id
		WHERE ($1 = '' OR owl.status = $1)
		  AND ($2 = '' OR owl.warehouse_id::text = $2)
		ORDER BY o.created_at DESC
		LIMIT 100`,
		status, warehouseID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, restaurantName, orderStatus, warehouseStatus, warehouseName, addrRaw string
		var total float64
		var createdAt time.Time
		var pickedAt, packedAt, dispatchedAt, deliveredAt *time.Time

		if err := rows.Scan(
			&id, &restaurantName, &total, &orderStatus, &createdAt, &addrRaw,
			&warehouseStatus, &warehouseName,
			&pickedAt, &packedAt, &dispatchedAt, &deliveredAt,
		); err != nil {
			continue
		}
		list = append(list, map[string]interface{}{
			"id":               id,
			"restaurant_name":  restaurantName,
			"total":            total,
			"order_status":     orderStatus,
			"warehouse_status": warehouseStatus,
			"warehouse_name":   warehouseName,
			"created_at":       createdAt,
			"picked_at":        pickedAt,
			"packed_at":        packedAt,
			"dispatched_at":    dispatchedAt,
			"delivered_at":     deliveredAt,
		})
	}
	return list, nil
}

// GetOrderDetail — single order ka full warehouse detail
func (s *Service) GetOrderDetail(orderID string) (map[string]interface{}, error) {
	var id, restaurantName, restaurantImg, orderStatus, paymentMethod string
	var total float64
	var createdAt time.Time
	var itemsRaw, addrRaw string

	err := database.DB.QueryRow(`
		SELECT id, restaurant_name, restaurant_image, status,
		       payment_method, items, delivery_address, total, created_at
		FROM orders WHERE id = $1`, orderID,
	).Scan(&id, &restaurantName, &restaurantImg, &orderStatus,
		&paymentMethod, &itemsRaw, &addrRaw, &total, &createdAt)
	if err != nil {
		return nil, err
	}

	// Warehouse log
	var warehouseStatus, warehouseName, notes string
	var pickedAt, packedAt, dispatchedAt, deliveredAt *time.Time
	database.DB.QueryRow(`
		SELECT COALESCE(owl.status,'pending'), COALESCE(w.name,'Unassigned'),
		       COALESCE(owl.notes,''),
		       owl.picked_at, owl.packed_at, owl.dispatched_at, owl.delivered_at
		FROM order_warehouse_log owl
		LEFT JOIN warehouse w ON w.id = owl.warehouse_id
		WHERE owl.order_id = $1`, orderID,
	).Scan(&warehouseStatus, &warehouseName, &notes,
		&pickedAt, &packedAt, &dispatchedAt, &deliveredAt)

	return map[string]interface{}{
		"id":               id,
		"restaurant_name":  restaurantName,
		"restaurant_image": restaurantImg,
		"order_status":     orderStatus,
		"payment_method":   paymentMethod,
		"total":            total,
		"created_at":       createdAt,
		"warehouse_status": warehouseStatus,
		"warehouse_name":   warehouseName,
		"notes":            notes,
		"picked_at":        pickedAt,
		"packed_at":        packedAt,
		"dispatched_at":    dispatchedAt,
		"delivered_at":     deliveredAt,
	}, nil
}

// warehouseStatusToOrderStatus — warehouse status ko Flutter wala order status mein convert karo
func warehouseStatusToOrderStatus(warehouseStatus string) string {
	switch warehouseStatus {
	case "picking":
		return "preparing"
	case "packed":
		return "preparing"
	case "dispatched":
		return "on_the_way"
	case "delivered":
		return "delivered"
	default:
		return "confirmed"
	}
}

// UpdateStatus — warehouse order status update karo + WebSocket broadcast
func (s *Service) UpdateStatus(orderID, status, notes string) error {
	// Check karo log exist karta hai ya nahi
	var logID string
	err := database.DB.QueryRow(
		`SELECT id FROM order_warehouse_log WHERE order_id = $1`, orderID,
	).Scan(&logID)

	now := time.Now()

	if err != nil {
		// Naya log banao — nearest warehouse assign karo
		var warehouseID string
		database.DB.QueryRow(
			`SELECT id FROM warehouse WHERE is_active = true LIMIT 1`,
		).Scan(&warehouseID)

		_, err = database.DB.Exec(`
			INSERT INTO order_warehouse_log
			  (order_id, warehouse_id, status, notes)
			VALUES ($1, $2, $3, $4)`,
			orderID, warehouseID, status, notes)

		if err == nil {
			// ✅ Broadcast karo
			order.GlobalHub.BroadcastStatus(orderID, order.TrackingPayload{
				OrderID: orderID,
				Status:  warehouseStatusToOrderStatus(status),
				Message: notes,
			})
		}
		return err
	}

	// Existing log update karo
	switch status {
	case "picking":
		_, err = database.DB.Exec(
			`UPDATE order_warehouse_log
			 SET status=$1, picked_at=$2, notes=$3
			 WHERE order_id=$4`,
			status, now, notes, orderID)

	case "packed":
		_, err = database.DB.Exec(
			`UPDATE order_warehouse_log
			 SET status=$1, packed_at=$2, notes=$3
			 WHERE order_id=$4`,
			status, now, notes, orderID)

	case "dispatched":
		_, err = database.DB.Exec(
			`UPDATE order_warehouse_log
			 SET status=$1, dispatched_at=$2, notes=$3
			 WHERE order_id=$4`,
			status, now, notes, orderID)

	case "delivered":
		_, err = database.DB.Exec(
			`UPDATE order_warehouse_log
			 SET status=$1, delivered_at=$2, notes=$3
			 WHERE order_id=$4`,
			status, now, notes, orderID)
		// Order status bhi update karo
		database.DB.Exec(
			`UPDATE orders SET status='delivered' WHERE id=$1`, orderID)

	default:
		_, err = database.DB.Exec(
			`UPDATE order_warehouse_log SET status=$1, notes=$2 WHERE order_id=$3`,
			status, notes, orderID)
	}

	// ✅ DB update successful hua toh Flutter ko broadcast karo
	if err == nil {
		// Driver location bhi fetch karo agar available hai
		var driverLat, driverLng *float64
		var driverName, driverPhone, driverImage string
		database.DB.QueryRow(`
			SELECT COALESCE(d.name,''), COALESCE(d.phone,''), COALESCE(d.image_url,''),
			       d.current_lat, d.current_lng
			FROM orders o
			LEFT JOIN drivers d ON d.id = o.driver_id
			WHERE o.id = $1`, orderID,
		).Scan(&driverName, &driverPhone, &driverImage, &driverLat, &driverLng)

		order.GlobalHub.BroadcastStatus(orderID, order.TrackingPayload{
			OrderID:     orderID,
			Status:      warehouseStatusToOrderStatus(status),
			DriverName:  driverName,
			DriverPhone: driverPhone,
			DriverImage: driverImage,
			DriverLat:   driverLat,
			DriverLng:   driverLng,
			Message:     notes,
		})
	}

	return err
}

// Stats — warehouse dashboard ke liye
func (s *Service) Stats() (map[string]interface{}, error) {
	stats := map[string]interface{}{}

	var todayOrders int
	database.DB.QueryRow(
		`SELECT COUNT(*) FROM orders WHERE DATE(created_at) = CURRENT_DATE`,
	).Scan(&todayOrders)

	rows, _ := database.DB.Query(`
		SELECT COALESCE(owl.status,'pending') as ws, COUNT(*)
		FROM orders o
		LEFT JOIN order_warehouse_log owl ON owl.order_id = o.id
		GROUP BY ws`)
	if rows != nil {
		defer rows.Close()
		statusCounts := map[string]int{}
		for rows.Next() {
			var s string
			var c int
			rows.Scan(&s, &c)
			statusCounts[s] = c
		}
		stats["by_status"] = statusCounts
	}

	var todayRevenue float64
	database.DB.QueryRow(
		`SELECT COALESCE(SUM(total),0) FROM orders
		 WHERE DATE(created_at) = CURRENT_DATE AND status != 'cancelled'`,
	).Scan(&todayRevenue)

	var totalOrders int
	database.DB.QueryRow(`SELECT COUNT(*) FROM orders`).Scan(&totalOrders)

	var totalRevenue float64
	database.DB.QueryRow(
		`SELECT COALESCE(SUM(total),0) FROM orders WHERE status != 'cancelled'`,
	).Scan(&totalRevenue)

	stats["today_orders"]  = todayOrders
	stats["today_revenue"] = todayRevenue
	stats["total_orders"]  = totalOrders
	stats["total_revenue"] = totalRevenue

	return stats, nil
}

// GetInventory — warehouse ke items
func (s *Service) GetInventory(warehouseID string) ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT i.id, w.name AS warehouse, f.name AS item,
		       i.quantity, i.min_quantity, i.unit, i.last_restocked,
		       CASE WHEN i.quantity <= i.min_quantity THEN true ELSE false END AS low_stock
		FROM inventory i
		JOIN warehouse w ON w.id = i.warehouse_id
		JOIN food_items f ON f.id = i.food_item_id
		WHERE ($1 = '' OR i.warehouse_id::text = $1)
		ORDER BY low_stock DESC, f.name`,
		warehouseID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, warehouseName, itemName, unit string
		var qty, minQty int
		var lowStock bool
		var lastRestocked time.Time
		if err := rows.Scan(&id, &warehouseName, &itemName,
			&qty, &minQty, &unit, &lastRestocked, &lowStock); err != nil {
			continue
		}
		list = append(list, map[string]interface{}{
			"id": id, "warehouse": warehouseName, "item": itemName,
			"quantity": qty, "min_quantity": minQty, "unit": unit,
			"last_restocked": lastRestocked, "low_stock": lowStock,
		})
	}
	return list, nil
}