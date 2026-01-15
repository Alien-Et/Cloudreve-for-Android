package explorer

import (
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/auth/requestinfo"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
)

type (
	ExplorerEventService struct {
		Uri string `form:"uri" binding:"required"`
	}
	ExplorerEventParamCtx struct{}
)

func (s *ExplorerEventService) HandleExplorerEventsPush(c *gin.Context) error {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	m := manager.NewFileManager(dep, user)
	l := logging.FromContext(c)
	defer m.Recycle()

	uri, err := fs.NewUriFromString(s.Uri)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Unknown uri", err)
	}

	// Make sure target is a valid folder that the user can listen to
	parent, _, err := m.List(c, uri, &manager.ListArgs{
		Page:     0,
		PageSize: 1,
	})
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Requested uri not available", err)
	}

	requestInfo := requestinfo.RequestInfoFromContext(c)
	if requestInfo.ClientID == "" {
		return serializer.NewError(serializer.CodeParamErr, "Client ID is required", nil)
	}

	// Client ID must be a valid UUID
	if _, err := uuid.FromString(requestInfo.ClientID); err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Invalid client ID", err)
	}

	// Subscribe
	eventHub := dep.EventHub()
	rx, resumed, err := eventHub.Subscribe(c, parent.ID(), requestInfo.ClientID)
	if err != nil {
		return serializer.NewError(serializer.CodeInternalSetting, "Failed to subscribe to events", err)
	}

	// SSE Headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	keepAliveTicker := time.NewTicker(30 * time.Second)
	defer keepAliveTicker.Stop()

	if resumed {
		c.SSEvent("resumed", nil)
		c.Writer.Flush()
	} else {
		c.SSEvent("subscribed", nil)
		c.Writer.Flush()
	}

	for {
		select {
		// TODO: close connection after access token expired
		case <-c.Request.Context().Done():
			// Server shutdown or request cancelled
			eventHub.Unsubscribe(c, parent.ID(), requestInfo.ClientID)
			l.Debug("Request context done, unsubscribed from event hub")
			return nil
		case <-c.Writer.CloseNotify():
			eventHub.Unsubscribe(c, parent.ID(), requestInfo.ClientID)
			l.Debug("Unsubscribed from event hub")
			return nil
		case evt, ok := <-rx:
			if !ok {
				// Channel closed, EventHub is shutting down
				l.Debug("Event hub closed, disconnecting client")
				return nil
			}
			c.SSEvent("event", evt)
			l.Debug("Event sent: %+v", evt)
			c.Writer.Flush()
		case <-keepAliveTicker.C:
			c.SSEvent("keep-alive", nil)
			c.Writer.Flush()
		}
	}
}
