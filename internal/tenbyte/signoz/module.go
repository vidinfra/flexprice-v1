package signoz

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.uber.org/fx"
)

// tracerProvider is stored to allow graceful shutdown
var tracerProvider *sdktrace.TracerProvider

// Module provides fx options for SigNoz/OpenTelemetry.
// Safe to include even when SigNoz is disabled.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewService),
		fx.Invoke(RegisterHooks),
	)
}

// RegisterHooks registers lifecycle hooks for SigNoz/OpenTelemetry.
// When SigNoz is disabled, this does nothing.
func RegisterHooks(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !svc.cfg.Tenbyte.SigNoz.Enabled {
				svc.logger.Info("SigNoz/OpenTelemetry is disabled")
				return nil
			}

			cfg := svc.cfg.Tenbyte.SigNoz

			// Validate required config
			if cfg.Endpoint == "" {
				svc.logger.Warn("SigNoz enabled but endpoint not configured, skipping initialization")
				return nil
			}

			// Create OTLP exporter options
			opts := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpoint(cfg.Endpoint),
			}
			if cfg.Insecure {
				opts = append(opts, otlptracegrpc.WithInsecure())
			}

			// Create exporter with timeout
			exportCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			exporter, err := otlptracegrpc.New(exportCtx, opts...)
			if err != nil {
				svc.logger.Errorw("Failed to create OTLP exporter, SigNoz tracing disabled",
					"error", err,
					"endpoint", cfg.Endpoint,
				)
				// Return nil to not block app startup - SigNoz is optional
				return nil
			}

			// Create resource with service info
			res, err := resource.Merge(
				resource.Default(),
				resource.NewWithAttributes(
					semconv.SchemaURL,
					semconv.ServiceName(cfg.ServiceName),
					semconv.DeploymentEnvironment(cfg.Environment),
					semconv.ServiceNamespace("tenbyte"),
				),
			)
			if err != nil {
				svc.logger.Errorw("Failed to create resource, using default", "error", err)
				res = resource.Default()
			}

			// Create tracer provider
			batchTimeout := time.Duration(cfg.BatchTimeout) * time.Millisecond
			exportTimeout := time.Duration(cfg.ExportTimeout) * time.Millisecond

			tp := sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(exporter,
					sdktrace.WithBatchTimeout(batchTimeout),
					sdktrace.WithExportTimeout(exportTimeout),
				),
				sdktrace.WithResource(res),
				sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRate)),
			)

			// Set as global tracer provider
			otel.SetTracerProvider(tp)
			otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			))

			// Store for shutdown and set on service
			tracerProvider = tp
			svc.setTracer(tp.Tracer(cfg.ServiceName))

			svc.logger.Infow("SigNoz/OpenTelemetry initialized successfully",
				"endpoint", cfg.Endpoint,
				"service_name", cfg.ServiceName,
				"environment", cfg.Environment,
				"sample_rate", cfg.SampleRate,
			)

			return nil
		},
		OnStop: func(ctx context.Context) error {
			if tracerProvider != nil {
				svc.logger.Info("Shutting down SigNoz/OpenTelemetry tracer")
				shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
					svc.logger.Errorw("Error shutting down tracer provider", "error", err)
					return err
				}
			}
			return nil
		},
	})
}
