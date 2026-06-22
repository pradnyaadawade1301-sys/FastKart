// internal/payments/payment_service.go
package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	razorpay "github.com/razorpay/razorpay-go"
)

type PaymentService struct {
	DB     *sql.DB
	Client *razorpay.Client
}

// NAYA - warning de aur aage badho
func NewPaymentService(db *sql.DB) *PaymentService {
    keyID := os.Getenv("RAZORPAY_KEY_ID")
    keySecret := os.Getenv("RAZORPAY_KEY_SECRET")
    if keyID == "" || keySecret == "" {
        log.Println("⚠️  RAZORPAY_KEY_ID / RAZORPAY_KEY_SECRET not set - payment disabled")
        return &PaymentService{DB: db, Client: nil}
    }
    client := razorpay.NewClient(keyID, keySecret)
    return &PaymentService{DB: db, Client: client}
}
func (s *PaymentService) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/payments/wallet/add", s.AddWalletBalance).Methods("POST")
	r.HandleFunc("/payments/wallet/balance", s.GetWalletBalance).Methods("GET")
	r.HandleFunc("/payments/wallet/transactions", s.GetTransactions).Methods("GET")

	r.HandleFunc("/payments/razorpay/order", s.CreateOrder).Methods("POST")
	r.HandleFunc("/payments/razorpay/verify", s.VerifyPayment).Methods("POST")
	r.HandleFunc("/payments/razorpay/refund", s.RefundPayment).Methods("POST")
	r.HandleFunc("/payments/razorpay/history", s.GetRazorpayTransactions).Methods("GET")
}

func (s *PaymentService) CreateOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		OrderID  string  `json:"order_id"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
		jsonError(w, "Valid amount required", http.StatusBadRequest)
		return
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}

	amountPaise := int64(req.Amount * 100)

	data := map[string]interface{}{
		"amount":   amountPaise,
		"currency": req.Currency,
		"receipt":  req.OrderID,
		"notes": map[string]interface{}{
			"order_id": req.OrderID,
			"user_id":  userID,
			"app":      "fastkart",
		},
	}

	rzpOrder, err := s.Client.Order.Create(data, nil)
	if err != nil {
		log.Printf("Razorpay order create error: %v", err)
		jsonError(w, "Payment initiation failed", http.StatusInternalServerError)
		return
	}

	rzpOrderID := rzpOrder["id"].(string)

	_, dbErr := s.DB.Exec(`
		INSERT INTO razorpay_transactions
			(id, user_id, order_id, razorpay_order_id, amount, currency, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,'created',NOW(),NOW())
		ON CONFLICT (razorpay_order_id) DO NOTHING`,
		uuid.New().String(), userID, req.OrderID, rzpOrderID, req.Amount, req.Currency,
	)
	if dbErr != nil {
		log.Printf("razorpay_transactions insert error: %v", dbErr)
	}

	jsonResp(w, map[string]interface{}{
		"razorpay_order_id": rzpOrderID,
		"amount":            req.Amount,
		"currency":          req.Currency,
		"key_id":            os.Getenv("RAZORPAY_KEY_ID"),
	})
}

func (s *PaymentService) VerifyPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderID           string `json:"order_id"`
		RazorpayOrderID   string `json:"razorpay_order_id"`
		RazorpayPaymentID string `json:"razorpay_payment_id"`
		RazorpaySignature string `json:"razorpay_signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.RazorpayOrderID == "" || req.RazorpayPaymentID == "" || req.RazorpaySignature == "" {
		jsonError(w, "razorpay_order_id, razorpay_payment_id, razorpay_signature required", http.StatusBadRequest)
		return
	}

	secret := os.Getenv("RAZORPAY_KEY_SECRET")
	if !verifySignature(req.RazorpayOrderID, req.RazorpayPaymentID, req.RazorpaySignature, secret) {
		jsonError(w, "Invalid payment signature", http.StatusPaymentRequired)
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		jsonError(w, "DB transaction failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE orders
		SET payment_status = 'paid', status = 'confirmed', updated_at = NOW()
		WHERE id = $1`, req.OrderID); err != nil {
		log.Printf("Order update error: %v", err)
		jsonError(w, "Order update failed", http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`
		UPDATE razorpay_transactions
		SET status = 'paid', razorpay_payment_id = $1, updated_at = NOW()
		WHERE razorpay_order_id = $2`, req.RazorpayPaymentID, req.RazorpayOrderID); err != nil {
		log.Printf("Transaction update error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	jsonResp(w, map[string]interface{}{
		"success":             true,
		"message":             "Payment verified successfully",
		"order_id":            req.OrderID,
		"razorpay_payment_id": req.RazorpayPaymentID,
		"status":              "paid",
	})
}

func verifySignature(orderID, paymentID, signature, secret string) bool {
	data := orderID + "|" + paymentID
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (s *PaymentService) RefundPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RazorpayPaymentID string  `json:"razorpay_payment_id"`
		AmountINR         float64 `json:"amount_inr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RazorpayPaymentID == "" {
		jsonError(w, "razorpay_payment_id required", http.StatusBadRequest)
		return
	}

	data := map[string]interface{}{
		"payment_id": req.RazorpayPaymentID,
	}
	if req.AmountINR > 0 {
		data["amount"] = int64(req.AmountINR * 100)
	}

	rf, err := s.Client.Refund.Create(data, nil)
	if err != nil {
		log.Printf("Razorpay refund error: %v", err)
		jsonError(w, "Refund failed", http.StatusInternalServerError)
		return
	}

	s.DB.Exec(`
		UPDATE razorpay_transactions
		SET status = 'refunded', updated_at = NOW()
		WHERE razorpay_payment_id = $1`, req.RazorpayPaymentID)

	refundedINR := req.AmountINR
	if refundedINR == 0 {
		if amt, ok := rf["amount"].(float64); ok {
			refundedINR = amt / 100
		}
	}

	jsonResp(w, map[string]interface{}{
		"success":   true,
		"refund_id": rf["id"],
		"status":    rf["status"],
		"amount":    refundedINR,
	})
}

func (s *PaymentService) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, "Request too large", http.StatusRequestEntityTooLarge)
		return
	}

	webhookSecret := os.Getenv("RAZORPAY_WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Println("❌ RAZORPAY_WEBHOOK_SECRET not set!")
		jsonError(w, "Webhook not configured", http.StatusInternalServerError)
		return
	}

	signature := r.Header.Get("X-Razorpay-Signature")
	h := hmac.New(sha256.New, []byte(webhookSecret))
	h.Write(payload)
	expected := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		jsonError(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	var event struct {
		Event   string `json:"event"`
		Payload struct {
			Payment struct {
				Entity struct {
					ID      string `json:"id"`
					OrderID string `json:"order_id"`
				} `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		jsonError(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	switch event.Event {
	case "payment.captured":
		s.DB.Exec(`
			UPDATE razorpay_transactions SET status='paid', razorpay_payment_id=$1, updated_at=NOW()
			WHERE razorpay_order_id=$2`, event.Payload.Payment.Entity.ID, event.Payload.Payment.Entity.OrderID)
		log.Printf("✅ Webhook: payment captured %s", event.Payload.Payment.Entity.ID)

	case "payment.failed":
		s.DB.Exec(`
			UPDATE razorpay_transactions SET status='failed', updated_at=NOW()
			WHERE razorpay_order_id=$1`, event.Payload.Payment.Entity.OrderID)
		log.Printf("❌ Webhook: payment failed %s", event.Payload.Payment.Entity.ID)

	default:
		log.Printf("ℹ️  Unhandled webhook event: %s", event.Event)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *PaymentService) GetRazorpayTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := s.DB.Query(`
		SELECT id, order_id, razorpay_order_id, amount, currency, status, created_at
		FROM razorpay_transactions
		WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		jsonError(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Txn struct {
		ID              string    `json:"id"`
		OrderID         string    `json:"order_id"`
		RazorpayOrderID string    `json:"razorpay_order_id"`
		Amount          float64   `json:"amount"`
		Currency        string    `json:"currency"`
		Status          string    `json:"status"`
		CreatedAt       time.Time `json:"created_at"`
	}

	txns := []Txn{}
	for rows.Next() {
		var t Txn
		rows.Scan(&t.ID, &t.OrderID, &t.RazorpayOrderID, &t.Amount, &t.Currency, &t.Status, &t.CreatedAt)
		txns = append(txns, t)
	}

	jsonResp(w, map[string]interface{}{
		"transactions": txns,
		"count":        len(txns),
	})
}

func (s *PaymentService) AddWalletBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount      float64 `json:"amount"`
		Description string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
		jsonError(w, "Valid amount required", http.StatusBadRequest)
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		jsonError(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var newBalance float64
	err = tx.QueryRow(`
		UPDATE users SET wallet_balance = wallet_balance + $1
		WHERE id = $2 RETURNING wallet_balance`,
		req.Amount, userID,
	).Scan(&newBalance)
	if err != nil {
		jsonError(w, "Wallet update failed", http.StatusInternalServerError)
		return
	}

	desc := req.Description
	if desc == "" {
		desc = "Wallet top-up"
	}

	tx.Exec(`
		INSERT INTO wallet_transactions (id, user_id, type, amount, description, created_at)
		VALUES ($1, $2, 'credit', $3, $4, NOW())`,
		uuid.New().String(), userID, req.Amount, desc,
	)

	tx.Commit()

	jsonResp(w, map[string]interface{}{
		"message":     "Wallet updated successfully",
		"new_balance": newBalance,
		"added":       req.Amount,
	})
}

func (s *PaymentService) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var balance float64
	var points int
	err := s.DB.QueryRow(`
		SELECT wallet_balance, points FROM users WHERE id = $1`, userID,
	).Scan(&balance, &points)
	if err != nil {
		jsonError(w, "User not found", http.StatusNotFound)
		return
	}

	jsonResp(w, map[string]interface{}{
		"balance": balance,
		"points":  points,
	})
}

func (s *PaymentService) GetTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := s.DB.Query(`
		SELECT id, type, amount, description, created_at
		FROM wallet_transactions
		WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		jsonError(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Txn struct {
		ID          string    `json:"id"`
		Type        string    `json:"type"`
		Amount      float64   `json:"amount"`
		Description string    `json:"description"`
		CreatedAt   time.Time `json:"created_at"`
	}

	txns := []Txn{}
	for rows.Next() {
		var t Txn
		rows.Scan(&t.ID, &t.Type, &t.Amount, &t.Description, &t.CreatedAt)
		txns = append(txns, t)
	}

	jsonResp(w, map[string]interface{}{
		"transactions": txns,
		"count":        len(txns),
	})
}

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
