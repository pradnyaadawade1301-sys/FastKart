// internal/warehouse/handler.go
package warehouse

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

// GET /api/warehouse/orders — saare orders with warehouse status
func (h *Handler) ListOrders(c *gin.Context) {
	status := c.Query("status")
	warehouseID := c.Query("warehouse_id")
	data, err := h.service.ListOrders(status, warehouseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// GET /api/warehouse/orders/:id — single order warehouse detail
func (h *Handler) GetOrder(c *gin.Context) {
	data, err := h.service.GetOrderDetail(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// PUT /api/warehouse/orders/:id/status — status update
func (h *Handler) UpdateStatus(c *gin.Context) {
	var req struct {
		Status string `json:"status" binding:"required"`
		Notes  string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.UpdateStatus(c.Param("id"), req.Status, req.Notes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Status updated to " + req.Status})
}

// GET /api/warehouse/stats — dashboard stats
func (h *Handler) Stats(c *gin.Context) {
	data, err := h.service.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// GET /api/warehouse/inventory — inventory list
func (h *Handler) Inventory(c *gin.Context) {
	warehouseID := c.Query("warehouse_id")
	data, err := h.service.GetInventory(warehouseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}