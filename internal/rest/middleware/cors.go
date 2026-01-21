package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware handles CORS headers
func CORSMiddleware(c *gin.Context) {
	origin := c.GetHeader("Origin")
	if origin == "" {
		origin = "*"
	}
	// Echo back the Origin when present (better for credentials and restrictive browsers)
	c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
	c.Writer.Header().Set("Vary", "Origin")

	c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

	// Prefer to echo requested headers when possible instead of using a wildcard
	reqHeaders := c.GetHeader("Access-Control-Request-Headers")
	if reqHeaders != "" {
		c.Writer.Header().Set("Access-Control-Allow-Headers", reqHeaders)
	} else {
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, Accept, Origin, X-Api-Key")
	}

	// Allow credentials if the client needs them (cookies, auth headers)
	c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
	c.Writer.Header().Set("Access-Control-Max-Age", "86400")

	if c.Request.Method == "OPTIONS" {
		c.AbortWithStatus(http.StatusOK)
		return
	}
	c.Next()
}
