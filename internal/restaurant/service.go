// internal/restaurant/service.go
package restaurant

import (
	"database/sql"

	"github.com/honor/fastkart-backend/pkg/database"
	"github.com/lib/pq"
)

type Service struct{}

func NewService() *Service { return &Service{} }

// List — all restaurants with optional category/search filter
func (s *Service) List(lat, lng float64, category, search string, page, limit int) ([]map[string]interface{}, error) {
	offset := (page - 1) * limit
	rows, err := database.DB.Query(`
		SELECT id, name, image_url, cover_url, rating, review_count,
		       delivery_time, delivery_fee, min_order, is_open,
		       categories, COALESCE(address,''), tags, COALESCE(description,'')
		FROM restaurants
		WHERE ($1 = '' OR $1 = ANY(categories))
		  AND ($2 = '' OR name ILIKE '%' || $2 || '%')
		ORDER BY rating DESC
		LIMIT $3 OFFSET $4`,
		category, search, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRestaurants(rows)
}

// GetByID — single restaurant detail
func (s *Service) GetByID(id string) (map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, image_url, cover_url, rating, review_count,
		       delivery_time, delivery_fee, min_order, is_open,
		       categories, COALESCE(address,''), tags, COALESCE(description,'')
		FROM restaurants WHERE id = $1`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanRestaurants(rows)
	if err != nil || len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
}

// GetMenu — food items for a restaurant
func (s *Service) GetMenu(restaurantID, category string) ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, COALESCE(description,''), COALESCE(image_url,''),
		       price, COALESCE(original_price,0),
		       COALESCE(category,''), is_popular, is_new, rating, sold_count
		FROM food_items
		WHERE restaurant_id = $1
		  AND ($2 = '' OR category = $2)
		  AND is_available = true
		ORDER BY category, is_popular DESC`,
		restaurantID, category,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var id, name, desc, imageURL, cat string
		var price, origPrice, rating float64
		var soldCount int
		var isPopular, isNew bool
		if err := rows.Scan(&id, &name, &desc, &imageURL, &price, &origPrice,
			&cat, &isPopular, &isNew, &rating, &soldCount); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id": id, "name": name, "description": desc,
			"image_url": imageURL, "price": price,
			"original_price": origPrice, "category": cat,
			"is_popular": isPopular, "is_new": isNew,
			"rating": rating, "sold_count": soldCount,
		})
	}
	return items, nil
}

// Nearby — restaurants within radiusKm using earthdistance
func (s *Service) Nearby(lat, lng, radiusKm float64) ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, image_url, rating, delivery_time, delivery_fee, is_open,
		       ROUND(
		         earth_distance(ll_to_earth(lat,lng), ll_to_earth($1,$2)) / 1000.0
		       , 1) AS dist_km
		FROM restaurants
		WHERE earth_box(ll_to_earth($1,$2), $3*1000) @> ll_to_earth(lat,lng)
		  AND is_open = true
		ORDER BY dist_km
		LIMIT 20`,
		lat, lng, radiusKm,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, name, imageURL, deliveryTime string
		var rating, deliveryFee, distKm float64
		var isOpen bool
		if err := rows.Scan(&id, &name, &imageURL, &rating,
			&deliveryTime, &deliveryFee, &isOpen, &distKm); err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"id": id, "name": name, "image_url": imageURL,
			"rating": rating, "delivery_time": deliveryTime,
			"delivery_fee": deliveryFee, "is_open": isOpen,
			"distance_km": distKm,
		})
	}
	return results, nil
}

// ── helper ────────────────────────────────────────────────────────────────────

type scannable interface {
	Next() bool
	Scan(dest ...interface{}) error
}

func scanRestaurants(rows scannable) ([]map[string]interface{}, error) {
	var list []map[string]interface{}
	for rows.Next() {
		var id, name, imageURL, coverURL, deliveryTime, address, desc string
		var rating, deliveryFee, minOrder float64
		var reviewCount int
		var isOpen bool
		var categories, tags pq.StringArray

		if err := rows.Scan(
			&id, &name, &imageURL, &coverURL,
			&rating, &reviewCount, &deliveryTime,
			&deliveryFee, &minOrder, &isOpen,
			&categories, &address, &tags, &desc,
		); err != nil {
			continue
		}
		list = append(list, map[string]interface{}{
			"id": id, "name": name, "image_url": imageURL,
			"cover_url": coverURL, "rating": rating,
			"review_count": reviewCount, "delivery_time": deliveryTime,
			"delivery_fee": deliveryFee, "min_order": minOrder,
			"is_open": isOpen, "categories": []string(categories),
			"address": address, "tags": []string(tags),
			"description": desc,
		})
	}
	return list, nil
}