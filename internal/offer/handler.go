// internal/offer/handler.go
package offer

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GET /api/offers
func (h *Handler) List(c *gin.Context) {
	offers, err := h.svc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "offers nahi mili"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "offers": offers})
}

// POST /api/offers/apply  { "code": "SAVE50", "cart_total": 499.0 }
func (h *Handler) Apply(c *gin.Context) {
	var body struct {
		Code      string  `json:"code" binding:"required"`
		CartTotal float64 `json:"cart_total" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.Apply(body.Code, body.CartTotal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "data": result})
}