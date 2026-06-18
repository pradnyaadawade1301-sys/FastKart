package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/honor/fastkart-backend/internal/auth"
	"github.com/honor/fastkart-backend/internal/middleware"
	"github.com/honor/fastkart-backend/internal/order"
	"github.com/honor/fastkart-backend/internal/payments"
	"github.com/honor/fastkart-backend/internal/restaurant"
	"github.com/honor/fastkart-backend/internal/user"
	"github.com/honor/fastkart-backend/internal/warehouse"
	"github.com/honor/fastkart-backend/pkg/database"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	database.Connect()
	defer database.DB.Close()

	r := gin.Default()

	// ── CORS ──────────────────────────────────────────────────────────────
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// ── Health ────────────────────────────────────────────────────────────
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "app": "FastKart API", "time": time.Now().Format(time.RFC3339)})
	})

	// ── Auth (public) ─────────────────────────────────────────────────────
	authH := auth.NewHandler(auth.NewService())
	r.POST("/auth/send-otp", authH.SendOTP)
	r.POST("/auth/verify-otp", authH.VerifyOTP)

	// ── Public restaurant routes ──────────────────────────────────────────
	restH := restaurant.NewHandler(restaurant.NewService())
	r.GET("/api/restaurants", restH.List)
	r.GET("/api/restaurants/nearby", restH.Nearby)
	r.GET("/api/restaurants/:id", restH.Get)
	r.GET("/api/restaurants/:id/menu", restH.Menu)

	// ── Payment service initialize karo ───────────────────────────────────
	paymentSvc := payments.NewPaymentService(database.DB)

	// ── Stripe Webhook — NO AUTH (Stripe JWT nahi bhejta) ─────────────────
	r.POST("/payments/stripe/webhook", gin.WrapF(paymentSvc.HandleWebhook))

	// ── Order service ─────────────────────────────────────────────────────
	orderSvc := order.NewService() // ✅ service alag banao handler se pehle

	// ── WebSocket — NO AUTH (Flutter WebSocket mein header bhejne mein dikkat hoti hai) ──
	r.GET("/api/orders/track/ws/:order_id", func(c *gin.Context) {
		orderSvc.TrackWebSocket(c.Writer, c.Request)
	})

	// ── Protected routes ──────────────────────────────────────────────────
	api := r.Group("/api", middleware.AuthRequired())
	{
		// User
		userH := user.NewHandler(user.NewService())
		api.GET("/me", userH.Me)
		api.PUT("/me", userH.Update)
		api.GET("/me/addresses", userH.Addresses)
		api.POST("/me/addresses", userH.AddAddress)
		api.GET("/me/wallet", userH.Wallet)

		// Orders — ✅ same orderSvc use karo
		orderH := order.NewHandler(orderSvc)
		api.POST("/orders", orderH.Place)
		api.GET("/orders", orderH.List)
		api.GET("/orders/:id", orderH.Get)
		api.PUT("/orders/:id/cancel", orderH.Cancel)
		api.GET("/orders/:id/track", orderH.Track)

		// Warehouse
		warehouseH := warehouse.NewHandler(warehouse.NewService())
		api.GET("/warehouse/orders", warehouseH.ListOrders)
		api.GET("/warehouse/orders/:id", warehouseH.GetOrder)
		api.PUT("/warehouse/orders/:id/status", warehouseH.UpdateStatus)
		api.GET("/warehouse/stats", warehouseH.Stats)
		api.GET("/warehouse/inventory", warehouseH.Inventory)

		// Stripe payments
		api.POST("/payments/stripe/intent", gin.WrapF(paymentSvc.CreatePaymentIntent))
		api.POST("/payments/stripe/verify", gin.WrapF(paymentSvc.VerifyPayment))
		api.POST("/payments/stripe/refund", gin.WrapF(paymentSvc.RefundPayment))
		api.GET("/payments/stripe/history", gin.WrapF(paymentSvc.GetStripeTransactions))
		// Wallet payments
		api.POST("/payments/wallet/add", gin.WrapF(paymentSvc.AddWalletBalance))
		api.GET("/payments/wallet/balance", gin.WrapF(paymentSvc.GetWalletBalance))
		api.GET("/payments/wallet/transactions", gin.WrapF(paymentSvc.GetTransactions))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 FastKart API running on :%s", port)
	log.Printf("🔌 WebSocket tracking: /api/orders/track/ws/:order_id")
	log.Printf("💳 Stripe routes: /api/payments/stripe/intent, /verify, /refund, /history")
	log.Printf("🔔 Stripe webhook: /payments/stripe/webhook (no auth)")

	// ✅ WebSocket ke liye standard net/http server use karo
	log.Fatal(http.ListenAndServe(":"+port, r))
}