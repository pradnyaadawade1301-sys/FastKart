package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/honor/fastkart-backend/internal/auth"
	"github.com/honor/fastkart-backend/internal/middleware"
	"github.com/honor/fastkart-backend/internal/notification"
	"github.com/honor/fastkart-backend/internal/offer"
	"github.com/honor/fastkart-backend/internal/order"
	"github.com/honor/fastkart-backend/internal/payments"
	"github.com/honor/fastkart-backend/internal/restaurant"
	"github.com/honor/fastkart-backend/internal/review"
	"github.com/honor/fastkart-backend/internal/search"
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

	// ── Public search ─────────────────────────────────────────────────────
	searchH := search.NewHandler(search.NewService())
	r.GET("/api/search", searchH.Search)

	// ── Payment service ───────────────────────────────────────────────────
	paymentSvc := payments.NewPaymentService(database.DB)


	// ── Order service ─────────────────────────────────────────────────────
	orderSvc := order.NewService()

	// ── WebSocket — NO AUTH ───────────────────────────────────────────────
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

		// Orders
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

		// Reviews
		reviewH := review.NewHandler(review.NewService())
		api.GET("/restaurants/:id/reviews", reviewH.List)
		api.POST("/restaurants/:id/reviews", reviewH.Create)
		api.PUT("/reviews/:id", reviewH.Update)
		api.DELETE("/reviews/:id", reviewH.Delete)

		// Offers / Coupons
		offerH := offer.NewHandler(offer.NewService())
		api.GET("/offers", offerH.List)
		api.POST("/offers/apply", offerH.Apply)

		// Notifications
		notifH := notification.NewHandler(notification.NewService())
		api.GET("/notifications", notifH.List)
		api.POST("/notifications/register", notifH.RegisterFCM)
		api.PUT("/notifications/:id/read", notifH.MarkRead)
		api.PUT("/notifications/read-all", notifH.MarkAllRead)

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
	log.Printf("🔍 Search: /api/search")
	log.Printf("⭐ Reviews: /api/restaurants/:id/reviews")
	log.Printf("🎁 Offers: /api/offers, /api/offers/apply")
	log.Printf("🔔 Notifications: /api/notifications")

	log.Fatal(http.ListenAndServe(":"+port, r))
}
