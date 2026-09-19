package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/service/message"
)

// MessageHandler handles message-related HTTP requests
type MessageHandler struct {
	service message.MessageService
}

// NewMessageHandler creates a new instance of MessageHandler
func NewMessageHandler(svc message.MessageService) *MessageHandler {
	return &MessageHandler{
		service: svc,
	}
}

// GetMessage handles GET /api/v1/message requests
func (h *MessageHandler) GetMessage(c *gin.Context) {
	msg, err := h.service.GetMessage(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}

	OK(c, http.StatusOK, "Message retrieved", msg)
}
