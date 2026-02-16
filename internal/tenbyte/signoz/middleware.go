// Package signoz provides OpenTelemetry-based observability for SigNoz.
// This middleware is isolated under internal/tenbyte/ to minimize conflicts
// when upgrading to newer versions of FlexPrice.
package signoz

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware returns a Gin middleware that traces all HTTP requests.
// Returns a no-op middleware if SigNoz is disabled.
func (s *Service) HTTPMiddleware() gin.HandlerFunc {
	if !s.IsEnabled() {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// Extract trace context from incoming request headers
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// Create span name from method and path
		spanName := fmt.Sprintf("%s %s", c.Request.Method, c.FullPath())
		if c.FullPath() == "" {
			spanName = fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path)
		}

		// Start span
		ctx, span := s.tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLPath(c.Request.URL.Path),
				semconv.URLScheme(c.Request.URL.Scheme),
				semconv.ServerAddress(c.Request.Host),
				semconv.UserAgentOriginal(c.Request.UserAgent()),
				semconv.ClientAddress(c.ClientIP()),
			),
		)
		defer span.End()

		// Add custom attributes
		if tenantID := c.GetHeader("x-tenant-id"); tenantID != "" {
			span.SetAttributes(attribute.String("tenant.id", tenantID))
		}
		if envID := c.GetHeader("x-environment-id"); envID != "" {
			span.SetAttributes(attribute.String("environment.id", envID))
		}

		// Update request context
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Record response status
		statusCode := c.Writer.Status()
		span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))

		// Mark as error if status >= 400
		if statusCode >= http.StatusBadRequest {
			span.SetAttributes(attribute.Bool("error", true))
		}

		// Record any errors from handlers
		if len(c.Errors) > 0 {
			span.SetAttributes(attribute.String("gin.errors", c.Errors.String()))
		}
	}
}
