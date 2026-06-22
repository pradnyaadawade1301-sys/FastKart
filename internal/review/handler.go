// internal/review/handler.go
package review

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

// GET /api/restaurants/:id/reviews
func (h *Handler) List(c *gin.Context) {
	restID := c.Param("id")
	reviews, avg, err := h.svc.List(restID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reviews nahi mili"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"reviews":        reviews,
		"average_rating": avg,
		"total":          len(reviews),
	})
}

// POST /api/restaurants/:id/reviews
func (h *Handler) Create(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}

	var body struct {
		Rating  int     `json:"rating" binding:"required,min=1,max=5"`
		Comment string  `json:"comment" binding:"required"`
		OrderID *string `json:"order_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	review, err := h.svc.Create(userID, c.Param("id"), body.Rating, body.Comment, body.OrderID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "ok", "review": review})
}

// PUT /api/reviews/:id
func (h *Handler) Update(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}

	var body struct {
		Rating  int    `json:"rating" binding:"required,min=1,max=5"`
		Comment string `json:"comment" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.Update(userID, c.Param("id"), body.Rating, body.Comment); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DELETE /api/reviews/:id
func (h *Handler) Delete(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}
	if err := h.svc.Delete(userID, c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}