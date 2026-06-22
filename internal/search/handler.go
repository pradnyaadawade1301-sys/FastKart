// internal/search/handler.go
package search

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GET /api/search?q=biryani&type=all
func (h *Handler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query 'q' required hai"})
		return
	}

	searchType := c.DefaultQuery("type", "all")
	switch searchType {
	case "all", "restaurant", "food":
		// valid
	default:
		searchType = "all"
	}

	result, err := h.svc.Search(q, searchType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"data":   result,
	})
}