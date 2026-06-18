// internal/auth/service.go
package auth

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/honor/fastkart-backend/pkg/database"
	"github.com/honor/fastkart-backend/pkg/jwt"
)

type Service struct{}

func NewService() *Service { return &Service{} }

// SendOTP — generates & stores a 6-digit OTP (expires in 5 min)
func (s *Service) SendOTP(phone string) (string, error) {
	otp := fmt.Sprintf("%06d", rand.Intn(900000)+100000)
	expiresAt := time.Now().Add(5 * time.Minute)

	_, err := database.DB.Exec(`
		INSERT INTO otp_logs (phone, otp, expires_at)
		VALUES ($1, $2, $3)`,
		phone, otp, expiresAt,
	)
	if err != nil {
		return "", err
	}

	// Dev mode: terminal mein print karo
	log.Printf("📱 OTP for %s: %s (expires at %s)", phone, otp, expiresAt.Format("15:04:05"))
	return otp, nil
}

// VerifyOTP — checks OTP, creates/fetches user, returns JWT token
func (s *Service) VerifyOTP(phone, otp, role string) (map[string]interface{}, error) {
	// OTP validate karo
	var logID string
	err := database.DB.QueryRow(`
		SELECT id FROM otp_logs
		WHERE phone = $1 AND otp = $2
		  AND is_used = false AND expires_at > NOW()
		ORDER BY created_at DESC LIMIT 1`,
		phone, otp,
	).Scan(&logID)
	if err != nil {
		return nil, errors.New("invalid or expired OTP")
	}

	// OTP mark as used
	database.DB.Exec(`UPDATE otp_logs SET is_used = true WHERE id = $1`, logID)

	// User fetch karo ya naya banao
	var userID, name, email, avatarURL string
	var walletBalance float64
	var points int

	err = database.DB.QueryRow(`
		SELECT id, COALESCE(name,''), COALESCE(email,''),
		       COALESCE(avatar_url,''), wallet_balance, points
		FROM users WHERE phone = $1`, phone,
	).Scan(&userID, &name, &email, &avatarURL, &walletBalance, &points)

	if err != nil {
		// Naya user banao
		err = database.DB.QueryRow(`
			INSERT INTO users (phone, role)
			VALUES ($1, $2)
			RETURNING id`,
			phone, role,
		).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		name = ""
		email = ""
		walletBalance = 0
		points = 0
	}

	// JWT token generate karo
	token, err := jwt.Generate(userID, phone, role)
	if err != nil {
		return nil, fmt.Errorf("token generation failed: %w", err)
	}

	return map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":             userID,
			"phone":          phone,
			"name":           name,
			"email":          email,
			"avatar_url":     avatarURL,
			"wallet_balance": walletBalance,
			"points":         points,
		},
	}, nil
}