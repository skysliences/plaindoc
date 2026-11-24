package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
)

// AuthClientIP 将可信代理解析后的来访地址传给认证业务。
func AuthClientIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithAuthClientIP(c.Request.Context(), c.ClientIP()))
		c.Next()
	}
}
