package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// OTELConfig holds OpenTelemetry configuration.
type OTELConfig struct {
	Endpoint    string  // OTLP HTTP endpoint — empty = no-op
	ServiceName string
	Version     string
	Environment string
	SampleRate  float64 // 0.0-1.0, default 1.0
}

// InitTracer creates an OTEL tracer provider and sets it as global.
// Returns a no-op shutdown func if Endpoint is empty.
func InitTracer(cfg OTELConfig) (shutdown func(context.Context) error, err error) {
	if cfg.Endpoint == "" {
		return func(ctx context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Version),
			semconv.DeploymentEnvironmentKey.String(cfg.Environment),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
