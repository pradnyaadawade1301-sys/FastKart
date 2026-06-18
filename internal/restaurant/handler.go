// internal/restaurant/handler.go
package restaurant

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

// GET /api/restaurants
func (h *Handler) List(c *gin.Context) {
	lat, _    := strconv.ParseFloat(c.Query("lat"), 64)
	lng, _    := strconv.ParseFloat(c.Query("lng"), 64)
	category  := c.Query("category")
	search    := c.Query("search")
	page, _   := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _  := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, err := h.service.List(lat, lng, category, search, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "page": page, "limit": limit})
}

// GET /api/restaurants/nearby
func (h *Handler) Nearby(c *gin.Context) {
	lat, _    := strconv.ParseFloat(c.Query("lat"), 64)
	lng, _    := strconv.ParseFloat(c.Query("lng"), 64)
	radius, _ := strconv.ParseFloat(c.DefaultQuery("radius", "5"), 64)

	data, err := h.service.Nearby(lat, lng, radius)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// GET /api/restaurants/:id
func (h *Handler) Get(c *gin.Context) {
	data, err := h.service.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Restaurant not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// GET /api/restaurants/:id/menu
func (h *Handler) Menu(c *gin.Context) {
	data, err := h.service.GetMenu(c.Param("id"), c.Query("category"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}