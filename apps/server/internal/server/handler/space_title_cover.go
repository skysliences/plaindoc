package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/view"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
)

// GetSpaceTitleCover renders the default cover with the same visibility rules as the space.
func (h *accessHandler) GetSpaceTitleCover(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	appendVaryHeader(c, "Cookie")
	appendVaryHeader(c, "Authorization")

	viewerID, err := h.resolveOptionalViewerUserID(c)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	space, err := h.visibilityService.GetSpace(c.Request.Context(), c.Param("spaceId"), viewerID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrSpaceAccessDenied),
			errors.Is(err, service.ErrViewerLoginRequired):
			c.Status(http.StatusNotFound)
		default:
			setRequestErrmsg(c, err, "读取空间封面失败")
			c.Status(http.StatusInternalServerError)
		}
		return
	}
	cover, err := view.RenderSpaceTitleCover(space.SpaceID, space.Name)
	if err != nil {
		setRequestErrmsg(c, err, "生成空间封面失败")
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "image/svg+xml; charset=utf-8", cover)
}
