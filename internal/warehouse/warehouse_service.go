package warehouse

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ─── Models ─────────────────────────────────────────────────

type InventoryItem struct {
	ID              string    `json:"id"`
	RestaurantID    string    `json:"restaurant_id"`
	MenuItemID      string    `json:"menu_item_id"`
	ItemName        string    `json:"item_name"`
	StockQuantity   int       `json:"stock_quantity"`
	MinStockAlert   int       `json:"min_stock_alert"`
	Unit            string    `json:"unit"`
	IsLowStock      bool      `json:"is_low_stock"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type InventoryLog struct {
	ID           string    `json:"id"`
	InventoryID  string    `json:"inventory_id"`
	MenuItemID   string    `json:"menu_item_id"`
	ItemName     string    `json:"item_name"`
	ChangeQty    int       `json:"change_qty"`
	Reason       string    `json:"reason"`
	OrderID      *string   `json:"order_id,omitempty"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
}

type RestockRequest struct {
	MenuItemID  string `json:"menu_item_id"`
	Quantity    int    `json:"quantity"`
	Notes       string `json:"notes"`
}

type WarehouseService struct {
	DB *sql.DB
}

// ─── Routes ─────────────────────────────────────────────────

func (w *WarehouseService) RegisterRoutes(r *mux.Router) {
	// Restaurant owner routes
	r.HandleFunc("/warehouse/{restaurant_id}/inventory", w.GetInventory).Methods("GET")
	r.HandleFunc("/warehouse/{restaurant_id}/inventory/{item_id}", w.GetItemStock).Methods("GET")
	r.HandleFunc("/warehouse/{restaurant_id}/restock", w.RestockItem).Methods("POST")
	r.HandleFunc("/warehouse/{restaurant_id}/logs", w.GetInventoryLogs).Methods("GET")
	r.HandleFunc("/warehouse/{restaurant_id}/low-stock", w.GetLowStockItems).Methods("GET")

	// Admin routes
	r.HandleFunc("/admin/warehouse/all", w.GetAllWarehouseStats).Methods("GET")
}

// ─── GET /warehouse/{restaurant_id}/inventory ───────────────
// Saare items ka current stock dikhata hai

func (w *WarehouseService) GetInventory(rw http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	restaurantID := vars["restaurant_id"]

	rows, err := w.DB.Query(`
		SELECT
			i.id, i.restaurant_id, i.menu_item_id,
			mi.name AS item_name,
			i.stock_quantity, i.min_stock_alert, i.unit,
			(i.stock_quantity <= i.min_stock_alert) AS is_low_stock,
			i.updated_at
		FROM inventory i
		JOIN menu_items mi ON mi.id = i.menu_item_id
		WHERE i.restaurant_id = $1
		ORDER BY mi.name
	`, restaurantID)
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []InventoryItem{}
	for rows.Next() {
		var item InventoryItem
		err := rows.Scan(
			&item.ID, &item.RestaurantID, &item.MenuItemID,
			&item.ItemName, &item.StockQuantity, &item.MinStockAlert,
			&item.Unit, &item.IsLowStock, &item.UpdatedAt,
		)
		if err != nil {
			continue
		}
		items = append(items, item)
	}

	jsonResp(rw, map[string]interface{}{
		"inventory":   items,
		"total_items": len(items),
	})
}

// ─── POST /warehouse/{restaurant_id}/restock ────────────────
// Stock add karo (delivery aayi, manual update)

func (w *WarehouseService) RestockItem(rw http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	restaurantID := vars["restaurant_id"]

	var req RestockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Quantity <= 0 {
		jsonError(rw, "Quantity must be positive", http.StatusBadRequest)
		return
	}

	tx, err := w.DB.Begin()
	if err != nil {
		jsonError(rw, "Transaction error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Check if inventory record exists
	var invID string
	err = tx.QueryRow(`
		SELECT id FROM inventory
		WHERE restaurant_id = $1 AND menu_item_id = $2
	`, restaurantID, req.MenuItemID).Scan(&invID)

	if err == sql.ErrNoRows {
		// Naya inventory record banao
		invID = uuid.New().String()
		_, err = tx.Exec(`
			INSERT INTO inventory (id, restaurant_id, menu_item_id, stock_quantity, min_stock_alert)
			VALUES ($1, $2, $3, $4, 5)
		`, invID, restaurantID, req.MenuItemID, req.Quantity)
	} else if err == nil {
		// Existing stock update karo
		_, err = tx.Exec(`
			UPDATE inventory
			SET stock_quantity = stock_quantity + $1, updated_at = NOW()
			WHERE id = $2
		`, req.Quantity, invID)
	}

	if err != nil {
		jsonError(rw, "Failed to update stock", http.StatusInternalServerError)
		return
	}

	// Log entry
	_, err = tx.Exec(`
		INSERT INTO inventory_log (id, inventory_id, menu_item_id, restaurant_id, change_qty, reason, notes)
		VALUES ($1, $2, $3, $4, $5, 'manual_restock', $6)
	`, uuid.New().String(), invID, req.MenuItemID, restaurantID, req.Quantity, req.Notes)

	if err != nil {
		jsonError(rw, "Failed to log restock", http.StatusInternalServerError)
		return
	}

	tx.Commit()

	// Return updated inventory
	var updated InventoryItem
	w.DB.QueryRow(`
		SELECT i.id, i.stock_quantity, i.updated_at, mi.name
		FROM inventory i JOIN menu_items mi ON mi.id = i.menu_item_id
		WHERE i.id = $1
	`, invID).Scan(&updated.ID, &updated.StockQuantity, &updated.UpdatedAt, &updated.ItemName)

	jsonResp(rw, map[string]interface{}{
		"message":       "Stock updated successfully",
		"new_stock":     updated.StockQuantity,
		"item_name":     updated.ItemName,
		"updated_at":    updated.UpdatedAt,
	})
}

// ─── GET /warehouse/{restaurant_id}/low-stock ───────────────
// Kam stock wale items

func (w *WarehouseService) GetLowStockItems(rw http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	restaurantID := vars["restaurant_id"]

	rows, err := w.DB.Query(`
		SELECT
			i.id, i.menu_item_id, mi.name,
			i.stock_quantity, i.min_stock_alert, i.unit
		FROM inventory i
		JOIN menu_items mi ON mi.id = i.menu_item_id
		WHERE i.restaurant_id = $1
		  AND i.stock_quantity <= i.min_stock_alert
		ORDER BY i.stock_quantity ASC
	`, restaurantID)
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []InventoryItem{}
	for rows.Next() {
		var item InventoryItem
		rows.Scan(&item.ID, &item.MenuItemID, &item.ItemName,
			&item.StockQuantity, &item.MinStockAlert, &item.Unit)
		item.IsLowStock = true
		items = append(items, item)
	}

	jsonResp(rw, map[string]interface{}{
		"low_stock_items": items,
		"count":           len(items),
		"alert":           len(items) > 0,
	})
}

// ─── GET /warehouse/{restaurant_id}/logs ────────────────────
// Saare stock changes ka history

func (w *WarehouseService) GetInventoryLogs(rw http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	restaurantID := vars["restaurant_id"]

	// Optional filters
	reason := r.URL.Query().Get("reason")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "50"
	}

	query := `
		SELECT
			il.id, il.inventory_id, il.menu_item_id,
			mi.name AS item_name,
			il.change_qty, il.reason, il.order_id,
			COALESCE(il.notes, '') AS notes,
			il.created_at
		FROM inventory_log il
		JOIN menu_items mi ON mi.id = il.menu_item_id
		WHERE il.restaurant_id = $1`

	args := []interface{}{restaurantID}

	if reason != "" {
		query += " AND il.reason = $2"
		args = append(args, reason)
		query += " ORDER BY il.created_at DESC LIMIT $3"
		args = append(args, limit)
	} else {
		query += " ORDER BY il.created_at DESC LIMIT $2"
		args = append(args, limit)
	}

	rows, err := w.DB.Query(query, args...)
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	logs := []InventoryLog{}
	for rows.Next() {
		var log InventoryLog
		rows.Scan(
			&log.ID, &log.InventoryID, &log.MenuItemID,
			&log.ItemName, &log.ChangeQty, &log.Reason,
			&log.OrderID, &log.Notes, &log.CreatedAt,
		)
		logs = append(logs, log)
	}

	jsonResp(rw, map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

// ─── GET /warehouse/{restaurant_id}/inventory/{item_id} ─────

func (w *WarehouseService) GetItemStock(rw http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	restaurantID := vars["restaurant_id"]
	itemID := vars["item_id"]

	var item InventoryItem
	err := w.DB.QueryRow(`
		SELECT i.id, i.restaurant_id, i.menu_item_id,
			mi.name, i.stock_quantity, i.min_stock_alert,
			i.unit, (i.stock_quantity <= i.min_stock_alert) AS is_low_stock,
			i.updated_at
		FROM inventory i
		JOIN menu_items mi ON mi.id = i.menu_item_id
		WHERE i.restaurant_id = $1 AND i.menu_item_id = $2
	`, restaurantID, itemID).Scan(
		&item.ID, &item.RestaurantID, &item.MenuItemID,
		&item.ItemName, &item.StockQuantity, &item.MinStockAlert,
		&item.Unit, &item.IsLowStock, &item.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		jsonResp(rw, map[string]interface{}{
			"stock_quantity": 0,
			"is_available":   false,
			"message":        "Item not in inventory",
		})
		return
	}
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}

	jsonResp(rw, item)
}

// ─── GET /admin/warehouse/all ───────────────────────────────

func (w *WarehouseService) GetAllWarehouseStats(rw http.ResponseWriter, r *http.Request) {
	rows, err := w.DB.Query(`
		SELECT
			r.id, r.name,
			COUNT(i.id) AS total_items,
			SUM(CASE WHEN i.stock_quantity <= i.min_stock_alert THEN 1 ELSE 0 END) AS low_stock_count,
			SUM(CASE WHEN i.stock_quantity = 0 THEN 1 ELSE 0 END) AS out_of_stock_count
		FROM restaurants r
		LEFT JOIN inventory i ON i.restaurant_id = r.id
		GROUP BY r.id, r.name
		ORDER BY low_stock_count DESC
	`)
	if err != nil {
		jsonError(rw, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RestaurantWarehouseStats struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		TotalItems      int    `json:"total_items"`
		LowStockCount   int    `json:"low_stock_count"`
		OutOfStockCount int    `json:"out_of_stock_count"`
	}

	stats := []RestaurantWarehouseStats{}
	for rows.Next() {
		var s RestaurantWarehouseStats
		rows.Scan(&s.ID, &s.Name, &s.TotalItems, &s.LowStockCount, &s.OutOfStockCount)
		stats = append(stats, s)
	}

	jsonResp(rw, map[string]interface{}{
		"restaurants": stats,
		"total_restaurants": len(stats),
	})
}

// ─── Helpers ────────────────────────────────────────────────

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}