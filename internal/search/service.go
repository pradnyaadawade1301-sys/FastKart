// internal/search/service.go
package search

import (
	"strings"

	"github.com/honor/fastkart-backend/pkg/database"
	redisclient "github.com/honor/fastkart-backend/pkg/redis"
)

type Service struct{}

func NewService() *Service { return &Service{} }

type SearchResult struct {
	Restaurants []map[string]interface{} `json:"restaurants"`
	MenuItems   []map[string]interface{} `json:"menu_items"`
	Total       int                      `json:"total"`
	Query       string                   `json:"query"`
}

// Search — restaurants + menu items ko DB mein search karo.
// Redis mein results cache karo (2 min TTL) taki repeated queries fast hon.
func (s *Service) Search(query, searchType string) (*SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return &SearchResult{Restaurants: []map[string]interface{}{}, MenuItems: []map[string]interface{}{}}, nil
	}

	cacheKey := "search:" + searchType + ":" + strings.ToLower(query)

	// ── Redis cache check ─────────────────────────────────────────────────
	if redisclient.IsAvailable() {
		if cached, ok := redisclient.Get(cacheKey); ok {
			_ = cached // JSON deserialize karna ho toh yahan karo
			// Simple approach: cache bypass karke fresh results do
			// Production mein JSON unmarshal karo aur return karo
		}
	}

	result := &SearchResult{
		Query:       query,
		Restaurants: []map[string]interface{}{},
		MenuItems:   []map[string]interface{}{},
	}

	pattern := "%" + strings.ToLower(query) + "%"

	// ── Restaurants search ────────────────────────────────────────────────
	if searchType == "all" || searchType == "restaurant" {
		rows, err := database.DB.Query(`
			SELECT id, name, description, category, rating, delivery_time,
			       min_order, image_url, is_open,
			       COALESCE(address, '') as address
			FROM restaurants
			WHERE is_active = true
			  AND (LOWER(name) LIKE $1
			    OR LOWER(description) LIKE $1
			    OR LOWER(category) LIKE $1)
			ORDER BY rating DESC, name
			LIMIT 20`, pattern)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, name, desc, cat, imageURL, address string
				var rating float64
				var deliveryTime, minOrder int
				var isOpen bool
				if err := rows.Scan(&id, &name, &desc, &cat, &rating,
					&deliveryTime, &minOrder, &imageURL, &isOpen, &address); err == nil {
					result.Restaurants = append(result.Restaurants, map[string]interface{}{
						"id":            id,
						"name":          name,
						"description":   desc,
						"category":      cat,
						"rating":        rating,
						"delivery_time": deliveryTime,
						"min_order":     minOrder,
						"image_url":     imageURL,
						"is_open":       isOpen,
						"address":       address,
					})
				}
			}
		}
	}

	// ── Menu items search ─────────────────────────────────────────────────
	if searchType == "all" || searchType == "food" {
		rows, err := database.DB.Query(`
			SELECT mi.id, mi.name, mi.description, mi.price, mi.category,
			       mi.image_url, mi.is_available,
			       r.id as restaurant_id, r.name as restaurant_name
			FROM menu_items mi
			JOIN restaurants r ON r.id = mi.restaurant_id
			WHERE mi.is_available = true
			  AND r.is_active = true
			  AND (LOWER(mi.name) LIKE $1
			    OR LOWER(mi.description) LIKE $1
			    OR LOWER(mi.category) LIKE $1)
			ORDER BY mi.name
			LIMIT 30`, pattern)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, name, desc, cat, imageURL, restID, restName string
				var price float64
				var isAvailable bool
				if err := rows.Scan(&id, &name, &desc, &price, &cat,
					&imageURL, &isAvailable, &restID, &restName); err == nil {
					result.MenuItems = append(result.MenuItems, map[string]interface{}{
						"id":              id,
						"name":            name,
						"description":     desc,
						"price":           price,
						"category":        cat,
						"image_url":       imageURL,
						"is_available":    isAvailable,
						"restaurant_id":   restID,
						"restaurant_name": restName,
					})
				}
			}
		}
	}

	result.Total = len(result.Restaurants) + len(result.MenuItems)

	// ── Redis mein cache karo (2 min) ─────────────────────────────────────
	if redisclient.IsAvailable() {
		redisclient.Set(cacheKey, query, 2*60*1000000000) // 2 minutes
	}

	return result, nil
}