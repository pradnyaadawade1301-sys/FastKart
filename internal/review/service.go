// internal/review/service.go
package review

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/honor/fastkart-backend/pkg/database"
)

type Service struct{}

func NewService() *Service { return &Service{} }

type Review struct {
	ID             string    `json:"id"`
	RestaurantID   string    `json:"restaurant_id"`
	UserID         string    `json:"user_id"`
	OrderID        *string   `json:"order_id,omitempty"`
	Rating         int       `json:"rating"`
	Comment        string    `json:"comment"`
	UserName       string    `json:"user_name"`
	CreatedAt      time.Time `json:"created_at"`
}

// List — restaurant ki saari reviews lo
func (s *Service) List(restaurantID string) ([]Review, float64, error) {
	rows, err := database.DB.Query(`
		SELECT r.id, r.restaurant_id, r.user_id, r.order_id,
		       r.rating, r.comment,
		       COALESCE(u.name, 'Anonymous') as user_name,
		       r.created_at
		FROM reviews r
		JOIN users u ON u.id = r.user_id
		WHERE r.restaurant_id = $1
		ORDER BY r.created_at DESC
		LIMIT 50`, restaurantID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var rv Review
		if err := rows.Scan(&rv.ID, &rv.RestaurantID, &rv.UserID, &rv.OrderID,
			&rv.Rating, &rv.Comment, &rv.UserName, &rv.CreatedAt); err == nil {
			reviews = append(reviews, rv)
		}
	}
	if reviews == nil {
		reviews = []Review{}
	}

	// Average rating calculate karo
	var avg float64
	database.DB.QueryRow(`
		SELECT COALESCE(AVG(rating), 0) FROM reviews WHERE restaurant_id = $1`,
		restaurantID).Scan(&avg)

	return reviews, avg, nil
}

// Create — naya review add karo
func (s *Service) Create(userID, restaurantID string, rating int, comment string, orderID *string) (*Review, error) {
	if rating < 1 || rating > 5 {
		return nil, errors.New("rating 1 se 5 ke beech honi chahiye")
	}

	// Check: user ne ek restaurant ko ek hi baar review kar sakta hai
	var existing string
	err := database.DB.QueryRow(`
		SELECT id FROM reviews WHERE user_id = $1 AND restaurant_id = $2`,
		userID, restaurantID).Scan(&existing)
	if err == nil {
		return nil, errors.New("aapne pehle se is restaurant ko review kiya hai")
	}

	id := uuid.New().String()
	now := time.Now()

	_, err = database.DB.Exec(`
		INSERT INTO reviews (id, restaurant_id, user_id, order_id, rating, comment, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		id, restaurantID, userID, orderID, rating, comment, now)
	if err != nil {
		return nil, err
	}

	// Restaurant ki average rating update karo
	database.DB.Exec(`
		UPDATE restaurants SET rating = (
			SELECT AVG(rating) FROM reviews WHERE restaurant_id = $1
		) WHERE id = $1`, restaurantID)

	var userName string
	database.DB.QueryRow(`SELECT COALESCE(name, 'Anonymous') FROM users WHERE id = $1`, userID).Scan(&userName)

	return &Review{
		ID:           id,
		RestaurantID: restaurantID,
		UserID:       userID,
		OrderID:      orderID,
		Rating:       rating,
		Comment:      comment,
		UserName:     userName,
		CreatedAt:    now,
	}, nil
}

// Update — apna review edit karo
func (s *Service) Update(userID, reviewID string, rating int, comment string) error {
	if rating < 1 || rating > 5 {
		return errors.New("rating 1 se 5 ke beech honi chahiye")
	}
	res, err := database.DB.Exec(`
		UPDATE reviews SET rating = $1, comment = $2, updated_at = NOW()
		WHERE id = $3 AND user_id = $4`,
		rating, comment, reviewID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("review nahi mili ya aapka nahi hai")
	}

	// Recalculate average
	var restaurantID string
	database.DB.QueryRow(`SELECT restaurant_id FROM reviews WHERE id = $1`, reviewID).Scan(&restaurantID)
	if restaurantID != "" {
		database.DB.Exec(`
			UPDATE restaurants SET rating = (
				SELECT AVG(rating) FROM reviews WHERE restaurant_id = $1
			) WHERE id = $1`, restaurantID)
	}
	return nil
}

// Delete — apna review delete karo
func (s *Service) Delete(userID, reviewID string) error {
	var restaurantID string
	database.DB.QueryRow(`SELECT restaurant_id FROM reviews WHERE id = $1 AND user_id = $2`,
		reviewID, userID).Scan(&restaurantID)

	res, err := database.DB.Exec(`DELETE FROM reviews WHERE id = $1 AND user_id = $2`,
		reviewID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("review nahi mili ya aapka nahi hai")
	}

	if restaurantID != "" {
		database.DB.Exec(`
			UPDATE restaurants SET rating = COALESCE((
				SELECT AVG(rating) FROM reviews WHERE restaurant_id = $1
			), 0) WHERE id = $1`, restaurantID)
	}
	return nil
}