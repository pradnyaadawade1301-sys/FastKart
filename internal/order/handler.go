package order

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

// POST /api/orders
func (h *Handler) Place(c *gin.Context) {
	var req PlaceOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserID = c.GetString("user_id")

	order, err := h.service.Place(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": order})
}

// GET /api/orders
func (h *Handler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Query("status")

	orders, err := h.service.ListByUser(userID, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": orders})
}

// GET /api/orders/:id
func (h *Handler) Get(c *gin.Context) {
	userID := c.GetString("user_id")
	order, err := h.service.GetByID(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": order})
}

// PUT /api/orders/:id/cancel
func (h *Handler) Cancel(c *gin.Context) {
	userID := c.GetString("user_id")
	if err := h.service.Cancel(c.Param("id"), userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Order cancelled"})
}

// GET /api/orders/:id/track
func (h *Handler) Track(c *gin.Context) {
	data, err := h.service.Track(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}