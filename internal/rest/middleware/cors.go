package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware returns a middleware that handles CORS headers with origin whitelisting
func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	// Build a set for O(1) lookup
	allowedSet := make(map[string]bool)
	for _, o := range allowedOrigins {
		allowedSet[o] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowCredentials := false

		// Check if origin is allowed - only echo specific origin when matched
		if origin != "" && (len(allowedOrigins) == 0 || allowedSet[origin]) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			allowCredentials = true // Safe to allow credentials with specific origin
		} else if len(allowedOrigins) == 0 {
			// Fallback to wildcard only when no origin header present
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			// Note: credentials not allowed with wildcard origin (browser rejects it)
		}
		c.Writer.Header().Set("Vary", "Origin")

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		// Prefer to echo requested headers when possible instead of using a wildcard
		reqHeaders := c.GetHeader("Access-Control-Request-Headers")
		if reqHeaders != "" {
			c.Writer.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, Accept, Origin, X-Api-Key")
		}

		// Only allow credentials when echoing a specific origin (not with wildcard)
		if allowCredentials {
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}
		c.Next()
	}
}
