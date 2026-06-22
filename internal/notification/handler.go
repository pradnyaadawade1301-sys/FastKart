// internal/notification/handler.go
package notification

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/honor/fastkart-backend/pkg/jwt"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func getUserID(c *gin.Context) string {
	claims, _ := c.Get("claims")
	if cl, ok := claims.(*jwt.Claims); ok {
		return cl.UserID
	}
	return ""
}

// GET /api/notifications
func (h *Handler) List(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}
	notifs, unread, _ := h.svc.List(userID)
	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"notifications": notifs,
		"unread_count":  unread,
	})
}

// POST /api/notifications/register  { "fcm_token": "...", "platform": "android" }
func (h *Handler) RegisterFCM(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}
	var body struct {
		FCMToken string `json:"fcm_token" binding:"required"`
		Platform string `json:"platform"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Platform == "" {
		body.Platform = "android"
	}
	if err := h.svc.RegisterFCM(userID, body.FCMToken, body.Platform); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token register nahi hua"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// PUT /api/notifications/:id/read
func (h *Handler) MarkRead(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}
	if err := h.svc.MarkRead(userID, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// PUT /api/notifications/read-all
func (h *Handler) MarkAllRead(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}
	h.svc.MarkAllRead(userID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}