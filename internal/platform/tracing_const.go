package platform

// Supported values for OTEL_TRACES_EXPORTER, mirrored from internal/tracing so
// that validating configuration does not drag the OpenTelemetry SDK into every
// package that reads config.
const (
	ExporterOTLP    = "otlp"
	ExporterConsole = "console"
)
