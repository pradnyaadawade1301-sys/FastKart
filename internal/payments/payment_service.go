// internal/payments/payment_service.go
package payments

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/paymentintent"
	stripRefund "github.com/stripe/stripe-go/v76/refund"
	"github.com/stripe/stripe-go/v76/webhook"
)

type PaymentService struct {
	DB *sql.DB
}

func NewPaymentService(db *sql.DB) *PaymentService {
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
	if stripe.Key == "" {
		log.Fatal("❌ STRIPE_SECRET_KEY not set in .env")
	}
	return &PaymentService{DB: db}
}

// RegisterRoutes — webhook ko auth middleware se BAHAR rakhna zaroori hai
// main.go mein:
//   public := r.PathPrefix("").Subrouter()
//   public.HandleFunc("/payments/stripe/webhook", paymentSvc.HandleWebhook).Methods("POST")
//   protected := r.PathPrefix("").Subrouter()
//   protected.Use(authMiddleware)
//   paymentSvc.RegisterRoutes(protected)
func (s *PaymentService) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/payments/wallet/add", s.AddWalletBalance).Methods("POST")
	r.HandleFunc("/payments/wallet/balance", s.GetWalletBalance).Methods("GET")
	r.HandleFunc("/payments/wallet/transactions", s.GetTransactions).Methods("GET")

	r.HandleFunc("/payments/stripe/intent", s.CreatePaymentIntent).Methods("POST")
	r.HandleFunc("/payments/stripe/verify", s.VerifyPayment).Methods("POST")
	r.HandleFunc("/payments/stripe/refund", s.RefundPayment).Methods("POST")
	r.HandleFunc("/payments/stripe/history", s.GetStripeTransactions).Methods("GET")
	// NOTE: /payments/stripe/webhook — yahan register MAT karo, main.go mein public subrouter pe karo
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /payments/stripe/intent
// ─────────────────────────────────────────────────────────────────────────────
func (s *PaymentService) CreatePaymentIntent(w http.ResponseWriter, r *http.Request) {
	// FIX 1: nil panic se bachao
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
		req.Currency = "inr"
	}

	amountPaise := int64(req.Amount * 100)

	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amountPaise),
		Currency: stripe.String(req.Currency),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
		Metadata: map[string]string{
			"order_id": req.OrderID,
			"user_id":  userID,
			"app":      "fastkart",
		},
	}

	pi, err := paymentintent.New(params)
	if err != nil {
		log.Printf("Stripe PaymentIntent error: %v", err)
		jsonError(w, "Payment initiation failed", http.StatusInternalServerError)
		return
	}

	// FIX 2: database.DB ki jagah s.DB use karo
	_, dbErr := s.DB.Exec(`
		INSERT INTO stripe_transactions
			(id, user_id, order_id, payment_intent_id, amount, currency, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,'pending',NOW(),NOW())
		ON CONFLICT (payment_intent_id) DO NOTHING`,
		uuid.New().String(), userID, req.OrderID, pi.ID, req.Amount, req.Currency,
	)
	if dbErr != nil {
		log.Printf("stripe_transactions insert error: %v", dbErr)
	}

	jsonResp(w, map[string]interface{}{
		"client_secret":     pi.ClientSecret,
		"payment_intent_id": pi.ID,
		"amount":            req.Amount,
		"currency":          req.Currency,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /payments/stripe/verify
// ─────────────────────────────────────────────────────────────────────────────
func (s *PaymentService) VerifyPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderID         string `json:"order_id"`
		PaymentIntentID string `json:"payment_intent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PaymentIntentID == "" {
		jsonError(w, "payment_intent_id required", http.StatusBadRequest)
		return
	}

	pi, err := paymentintent.Get(req.PaymentIntentID, nil)
	if err != nil {
		log.Printf("Stripe intent fetch error: %v", err)
		jsonError(w, "Could not verify payment", http.StatusBadGateway)
		return
	}

	if pi.Status != stripe.PaymentIntentStatusSucceeded {
		jsonError(w, "Payment not completed. Status: "+string(pi.Status), http.StatusPaymentRequired)
		return
	}

	// FIX 3: transaction error properly handle karo
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
		UPDATE stripe_transactions
		SET status = 'succeeded', updated_at = NOW()
		WHERE payment_intent_id = $1`, req.PaymentIntentID); err != nil {
		log.Printf("Transaction update error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	jsonResp(w, map[string]interface{}{
		"success":           true,
		"message":           "Payment verified successfully",
		"order_id":          req.OrderID,
		"payment_intent_id": req.PaymentIntentID,
		"status":            "paid",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /payments/stripe/refund
// ─────────────────────────────────────────────────────────────────────────────
func (s *PaymentService) RefundPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PaymentIntentID string  `json:"payment_intent_id"`
		AmountINR       float64 `json:"amount_inr"`
		Reason          string  `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PaymentIntentID == "" {
		jsonError(w, "payment_intent_id required", http.StatusBadRequest)
		return
	}

	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(req.PaymentIntentID),
	}
	if req.AmountINR > 0 {
		params.Amount = stripe.Int64(int64(req.AmountINR * 100))
	}
	switch req.Reason {
	case "duplicate":
		params.Reason = stripe.String(string(stripe.RefundReasonDuplicate))
	case "fraudulent":
		params.Reason = stripe.String(string(stripe.RefundReasonFraudulent))
	default:
		params.Reason = stripe.String(string(stripe.RefundReasonRequestedByCustomer))
	}

	rf, err := stripRefund.New(params)
	if err != nil {
		log.Printf("Stripe refund error: %v", err)
		jsonError(w, "Refund failed", http.StatusInternalServerError)
		return
	}

	// FIX 2: s.DB use karo
	s.DB.Exec(`
		UPDATE stripe_transactions
		SET status = 'refunded', updated_at = NOW()
		WHERE payment_intent_id = $1`, req.PaymentIntentID)

	refundedINR := req.AmountINR
	if refundedINR == 0 {
		refundedINR = float64(rf.Amount) / 100
	}

	jsonResp(w, map[string]interface{}{
		"success":   true,
		"refund_id": rf.ID,
		"status":    string(rf.Status),
		"amount":    refundedINR,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /payments/stripe/webhook  — auth middleware BYPASS karke register karo
// ─────────────────────────────────────────────────────────────────────────────
func (s *PaymentService) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, "Request too large", http.StatusRequestEntityTooLarge)
		return
	}

	// FIX 4: webhook secret empty check
	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Println("❌ STRIPE_WEBHOOK_SECRET not set!")
		jsonError(w, "Webhook not configured", http.StatusInternalServerError)
		return
	}

	event, err := webhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), webhookSecret)
	if err != nil {
		log.Printf("Webhook signature invalid: %v", err)
		jsonError(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "payment_intent.succeeded":
		piID, _ := event.Data.Object["id"].(string)
		s.DB.Exec(`
			UPDATE stripe_transactions SET status='succeeded', updated_at=NOW()
			WHERE payment_intent_id=$1`, piID)
		log.Printf("✅ Webhook: payment succeeded %s", piID)

	case "payment_intent.payment_failed":
		piID, _ := event.Data.Object["id"].(string)
		s.DB.Exec(`
			UPDATE stripe_transactions SET status='failed', updated_at=NOW()
			WHERE payment_intent_id=$1`, piID)
		log.Printf("❌ Webhook: payment failed %s", piID)

	case "charge.refunded":
		log.Printf("💸 Webhook: charge refunded %v", event.Data.Object["id"])

	default:
		log.Printf("ℹ️  Unhandled webhook event: %s", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /payments/stripe/history
// ─────────────────────────────────────────────────────────────────────────────
func (s *PaymentService) GetStripeTransactions(w http.ResponseWriter, r *http.Request) {
	// FIX 1: nil panic se bachao
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := s.DB.Query(`
		SELECT id, order_id, payment_intent_id, amount, currency, status, created_at
		FROM stripe_transactions
		WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		jsonError(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type StripeTxn struct {
		ID              string    `json:"id"`
		OrderID         string    `json:"order_id"`
		PaymentIntentID string    `json:"payment_intent_id"`
		Amount          float64   `json:"amount"`
		Currency        string    `json:"currency"`
		Status          string    `json:"status"`
		CreatedAt       time.Time `json:"created_at"`
	}

	txns := []StripeTxn{}
	for rows.Next() {
		var t StripeTxn
		rows.Scan(&t.ID, &t.OrderID, &t.PaymentIntentID, &t.Amount, &t.Currency, &t.Status, &t.CreatedAt)
		txns = append(txns, t)
	}

	jsonResp(w, map[string]interface{}{
		"transactions": txns,
		"count":        len(txns),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Wallet handlers
// ─────────────────────────────────────────────────────────────────────────────

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