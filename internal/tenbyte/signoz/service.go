// Package signoz provides OpenTelemetry-based observability for SigNoz.
// This package is isolated under internal/tenbyte/ to minimize conflicts
// when upgrading to newer versions of FlexPrice.
package signoz

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// noopTracer is used when SigNoz is disabled
var noopTracer = noop.NewTracerProvider().Tracer("noop")

// Service provides OpenTelemetry tracing and metrics for SigNoz.
// All methods are safe to call even when SigNoz is disabled - they will
// return no-op implementations to ensure existing flows are not affected.
type Service struct {
	cfg    *config.Configuration
	logger *logger.Logger
	tracer trace.Tracer
	meter  metric.Meter
}

// NewService creates a new SigNoz service.
// The service is safe to use even when SigNoz is disabled.
func NewService(cfg *config.Configuration, logger *logger.Logger) *Service {
	return &Service{
		cfg:    cfg,
		logger: logger,
		tracer: noopTracer, // Default to noop tracer
	}
}

// IsEnabled returns true if SigNoz/OpenTelemetry is enabled
func (s *Service) IsEnabled() bool {
	return s.cfg.Tenbyte.SigNoz.Enabled
}

// setTracer sets the tracer (called during initialization)
func (s *Service) setTracer(tracer trace.Tracer) {
	s.tracer = tracer
}

// setMeter sets the meter (called during initialization)
func (s *Service) setMeter(meter metric.Meter) {
	s.meter = meter
}

// StartSpan starts a new span with the given operation name.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartSpan(ctx context.Context, operationName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if !s.IsEnabled() {
		return noopTracer.Start(ctx, operationName, opts...)
	}
	return s.tracer.Start(ctx, operationName, opts...)
}

// StartDBSpan starts a database operation span for Postgres.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartDBSpan(ctx context.Context, operation string, params map[string]interface{}) (context.Context, trace.Span) {
	if !s.IsEnabled() {
		return noopTracer.Start(ctx, "db.postgres."+operation)
	}

	attrs := []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", operation),
	}
	for k, v := range params {
		attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", v)))
	}

	return s.tracer.Start(ctx, "db.postgres."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
}

// StartClickHouseSpan starts a ClickHouse operation span.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartClickHouseSpan(ctx context.Context, operation string, params map[string]interface{}) (context.Context, trace.Span) {
	if !s.IsEnabled() {
		return noopTracer.Start(ctx, "db.clickhouse."+operation)
	}

	attrs := []attribute.KeyValue{
		attribute.String("db.system", "clickhouse"),
		attribute.String("db.operation", operation),
	}
	for k, v := range params {
		attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", v)))
	}

	return s.tracer.Start(ctx, "db.clickhouse."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
}

// StartKafkaConsumerSpan starts a Kafka consumer span.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartKafkaConsumerSpan(ctx context.Context, topic string) (context.Context, trace.Span) {
	if !s.IsEnabled() {
		return noopTracer.Start(ctx, "kafka.consume."+topic)
	}

	return s.tracer.Start(ctx, "kafka.consume."+topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", topic),
			attribute.String("messaging.operation", "receive"),
		),
	)
}

// StartKafkaProducerSpan starts a Kafka producer span.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartKafkaProducerSpan(ctx context.Context, topic string) (context.Context, trace.Span) {
	if !s.IsEnabled() {
		return noopTracer.Start(ctx, "kafka.produce."+topic)
	}

	return s.tracer.Start(ctx, "kafka.produce."+topic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", topic),
			attribute.String("messaging.operation", "send"),
		),
	)
}

// MonitorEventProcessing tracks event processing with lag metrics.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) MonitorEventProcessing(ctx context.Context, eventName string, eventTimestamp time.Time, metadata map[string]interface{}) (context.Context, trace.Span) {
	if !s.IsEnabled() {
		return noopTracer.Start(ctx, "event.process")
	}

	lag := time.Since(eventTimestamp)
	lagMs := lag.Milliseconds()

	// Determine severity based on lag
	severity := "normal"
	if lagMs >= 5*60*1000 { // 5 minutes
		severity = "critical"
	} else if lagMs >= 60*1000 { // 1 minute
		severity = "warning"
	}

	ctx, span := s.tracer.Start(ctx, "event.process",
		trace.WithAttributes(
			attribute.String("event.name", eventName),
			attribute.Int64("event.lag_ms", lagMs),
			attribute.String("event.lag.severity", severity),
		),
	)

	for k, v := range metadata {
		span.SetAttributes(attribute.String(k, fmt.Sprintf("%v", v)))
	}

	return ctx, span
}

// StartRepositorySpan creates a span for a repository operation.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartRepositorySpan(ctx context.Context, repository, operation string, params map[string]interface{}) (context.Context, trace.Span) {
	operationName := fmt.Sprintf("repository.%s.%s", repository, operation)

	if !s.IsEnabled() {
		return noopTracer.Start(ctx, operationName)
	}

	attrs := []attribute.KeyValue{
		attribute.String("repository", repository),
		attribute.String("operation", operation),
	}

	for k, v := range params {
		attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", v)))
	}

	return s.tracer.Start(ctx, operationName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

// StartBillingSpan creates a span for billing operations.
// Returns a no-op span if SigNoz is disabled.
func (s *Service) StartBillingSpan(ctx context.Context, operation string, params map[string]interface{}) (context.Context, trace.Span) {
	operationName := "billing." + operation

	if !s.IsEnabled() {
		return noopTracer.Start(ctx, operationName)
	}

	attrs := []attribute.KeyValue{
		attribute.String("billing.operation", operation),
	}

	for k, v := range params {
		attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", v)))
	}

	return s.tracer.Start(ctx, operationName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

// RecordError records an error on the given span.
// Safe to call with nil span or when SigNoz is disabled.
func (s *Service) RecordError(span trace.Span, err error) {
	if span == nil || !s.IsEnabled() || !span.IsRecording() {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanSuccess marks a span as successful.
// Safe to call with nil span or when SigNoz is disabled.
func (s *Service) SetSpanSuccess(span trace.Span) {
	if span == nil || !s.IsEnabled() || !span.IsRecording() {
		return
	}
	span.SetStatus(codes.Ok, "success")
}

// Tenbyte: Attribute helpers for organization-wise event tracking

// OrganizationID returns an attribute for organization/external customer ID
func OrganizationID(id string) attribute.KeyValue {
	return attribute.String("organization.id", id)
}

// EventName returns an attribute for event name
func EventName(name string) attribute.KeyValue {
	return attribute.String("event.name", name)
}

// EventID returns an attribute for event ID
func EventID(id string) attribute.KeyValue {
	return attribute.String("event.id", id)
}

// TenantID returns an attribute for tenant ID
func TenantID(id string) attribute.KeyValue {
	return attribute.String("tenant.id", id)
}

// EnvironmentID returns an attribute for environment ID
func EnvironmentID(id string) attribute.KeyValue {
	return attribute.String("environment.id", id)
}

// EventStatus returns an attribute for event processing status
func EventStatus(status string) attribute.KeyValue {
	return attribute.String("event.status", status)
}

// EventError returns an attribute for event processing error
func EventError(err string) attribute.KeyValue {
	return attribute.String("event.error", err)
}

// EventProperty returns an attribute for an event property with proper type handling
func EventProperty(key string, value interface{}) attribute.KeyValue {
	attrKey := "event.property." + key
	switch v := value.(type) {
	case float64:
		return attribute.Float64(attrKey, v)
	case int:
		return attribute.Int(attrKey, v)
	case int64:
		return attribute.Int64(attrKey, v)
	case string:
		return attribute.String(attrKey, v)
	case bool:
		return attribute.Bool(attrKey, v)
	default:
		return attribute.String(attrKey, fmt.Sprintf("%v", v))
	}
}
