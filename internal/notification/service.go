// internal/notification/service.go
package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/honor/fastkart-backend/pkg/database"
)

type Service struct{}

func NewService() *Service { return &Service{} }

type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Type      string    `json:"type"` // order_update, offer, general
	Data      string    `json:"data"` // JSON string for extra info
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

// List — user ki saari notifications lo
func (s *Service) List(userID string) ([]Notification, int, error) {
	rows, err := database.DB.Query(`
		SELECT id, user_id, title, body, type, COALESCE(data, '{}'), is_read, created_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 50`, userID)
	if err != nil {
		return []Notification{}, 0, nil
	}
	defer rows.Close()

	var notifs []Notification
	unread := 0
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Body,
			&n.Type, &n.Data, &n.IsRead, &n.CreatedAt); err == nil {
			notifs = append(notifs, n)
			if !n.IsRead {
				unread++
			}
		}
	}
	if notifs == nil {
		notifs = []Notification{}
	}
	return notifs, unread, nil
}

// RegisterFCM — user ka FCM token save karo
func (s *Service) RegisterFCM(userID, fcmToken, platform string) error {
	_, err := database.DB.Exec(`
		INSERT INTO fcm_tokens (id, user_id, token, platform, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (user_id, platform) DO UPDATE
		  SET token = $3, updated_at = NOW()`,
		uuid.New().String(), userID, fcmToken, platform)
	return err
}

// MarkRead — ek notification ko read mark karo
func (s *Service) MarkRead(userID, notifID string) error {
	_, err := database.DB.Exec(`
		UPDATE notifications SET is_read = true
		WHERE id = $1 AND user_id = $2`, notifID, userID)
	return err
}

// MarkAllRead — user ki saari notifications read mark karo
func (s *Service) MarkAllRead(userID string) error {
	_, err := database.DB.Exec(`
		UPDATE notifications SET is_read = true WHERE user_id = $1`, userID)
	return err
}

// Send — ek user ko notification bhejo (DB save + FCM push)
func (s *Service) Send(userID, title, body, notifType string, data map[string]string) error {
	// DB mein save karo
	dataJSON, _ := json.Marshal(data)
	_, err := database.DB.Exec(`
		INSERT INTO notifications (id, user_id, title, body, type, data, is_read, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, false, NOW())`,
		uuid.New().String(), userID, title, body, notifType, string(dataJSON))
	if err != nil {
		log.Printf("⚠️  Notification DB save failed: %v", err)
	}

	// FCM push karo (Firebase se token lo)
	var fcmToken string
	database.DB.QueryRow(`SELECT token FROM fcm_tokens WHERE user_id = $1 ORDER BY updated_at DESC LIMIT 1`,
		userID).Scan(&fcmToken)

	if fcmToken != "" {
		go sendFCM(fcmToken, title, body, data)
	}
	return nil
}

// SendOrderUpdate — order status change hone pe notification
func (s *Service) SendOrderUpdate(userID, orderID, status string) {
	titles := map[string]string{
		"confirmed":   "Order Confirm Ho Gaya! 🎉",
		"preparing":   "Aapka Khana Ban Raha Hai 👨‍🍳",
		"out_for_delivery": "Rider Aa Raha Hai 🛵",
		"delivered":   "Order Deliver Ho Gaya! ✅",
		"cancelled":   "Order Cancel Ho Gaya ❌",
	}
	bodies := map[string]string{
		"confirmed":   "Aapka order confirm ho gaya. Jaldi tayar hoga!",
		"preparing":   "Restaurant aapka khana bana raha hai.",
		"out_for_delivery": "Rider aapke order ke saath nikal pada!",
		"delivered":   "Aapka order deliver ho gaya. Enjoy karo!",
		"cancelled":   "Aapka order cancel ho gaya. Refund process hoga.",
	}
	title := titles[status]
	body := bodies[status]
	if title == "" {
		title = "Order Update"
		body = fmt.Sprintf("Order #%s ka status: %s", orderID[:8], status)
	}
	s.Send(userID, title, body, "order_update", map[string]string{
		"order_id": orderID,
		"status":   status,
	})
}

// sendFCM — Firebase Cloud Messaging pe push notification bhejo
func sendFCM(token, title, body string, data map[string]string) {
	serverKey := os.Getenv("FCM_SERVER_KEY")
	if serverKey == "" {
		log.Println("⚠️  FCM_SERVER_KEY not set — push skip")
		return
	}

	payload := map[string]interface{}{
		"to": token,
		"notification": map[string]string{
			"title": title,
			"body":  body,
			"sound": "default",
		},
		"data": data,
	}

	jsonBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://fcm.googleapis.com/fcm/send",
		bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "key="+serverKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️  FCM send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("📱 FCM sent, status: %d", resp.StatusCode)
}