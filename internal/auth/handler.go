// internal/auth/handler.go
package auth

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

// POST /auth/send-otp
func (h *Handler) SendOTP(c *gin.Context) {
	var req struct {
		Phone string `json:"phone" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone number required"})
		return
	}
	otp, err := h.service.SendOTP(req.Phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Dev mode mein OTP response mein bhi bhejo
	c.JSON(http.StatusOK, gin.H{
		"message": "OTP sent successfully",
		"otp":     otp, // Production mein yeh hataao
	})
}

// POST /auth/verify-otp
func (h *Handler) VerifyOTP(c *gin.Context) {
	var req struct {
		Phone string `json:"phone" binding:"required"`
		OTP   string `json:"otp"   binding:"required"`
		Role  string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role == "" {
		req.Role = "customer"
	}
	result, err := h.service.VerifyOTP(req.Phone, req.OTP, req.Role)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
