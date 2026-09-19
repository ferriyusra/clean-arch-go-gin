package tracing

import (
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// gormTracerName identifies the instrumentation in the tracing backend.
const gormTracerName = "github.com/ferriyusra/clean-arch-go-gin/internal/tracing"

// GormPlugin emits one span per database statement, as a child of whatever span
// is on the statement context.
//
// This is hand-written rather than pulled from gorm.io/plugin/opentelemetry
// because that package links a ClickHouse driver, lz4 and a compression library
// into the binary in order to name the database system. Twenty-four extra
// packages is a poor trade for an attribute we can set ourselves, and writing
// it out also makes it obvious that query *variables* are never recorded.
type GormPlugin struct{}

// NewGormPlugin returns the plugin. Install it with db.Use(...).
func NewGormPlugin() gorm.Plugin { return GormPlugin{} }

// Name implements gorm.Plugin.
func (GormPlugin) Name() string { return "otel-tracing" }

// Initialize registers a before/after callback pair for every statement kind.
// Initialize registers a before/after callback pair for every statement kind.
//
// The registrations are written out rather than looped because the processor
// type returned by db.Callback().Create() is unexported, so it cannot be held
// in a variable or passed to a helper.
func (p GormPlugin) Initialize(db *gorm.DB) error {
	return errors.Join(
		db.Callback().Create().Before("gorm:create").Register("otel:before_create", startSpan("create")),
		db.Callback().Create().After("gorm:create").Register("otel:after_create", endSpan),

		db.Callback().Query().Before("gorm:query").Register("otel:before_query", startSpan("query")),
		db.Callback().Query().After("gorm:query").Register("otel:after_query", endSpan),

		db.Callback().Update().Before("gorm:update").Register("otel:before_update", startSpan("update")),
		db.Callback().Update().After("gorm:update").Register("otel:after_update", endSpan),

		db.Callback().Delete().Before("gorm:delete").Register("otel:before_delete", startSpan("delete")),
		db.Callback().Delete().After("gorm:delete").Register("otel:after_delete", endSpan),

		db.Callback().Row().Before("gorm:row").Register("otel:before_row", startSpan("row")),
		db.Callback().Row().After("gorm:row").Register("otel:after_row", endSpan),

		db.Callback().Raw().Before("gorm:raw").Register("otel:before_raw", startSpan("raw")),
		db.Callback().Raw().After("gorm:raw").Register("otel:after_raw", endSpan),
	)
}

func startSpan(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		if db.Statement == nil || db.Statement.Context == nil {
			return
		}

		// No parent span means the call did not come from a traced request, so
		// a span here would be an orphan in the trace store.
		if !trace.SpanContextFromContext(db.Statement.Context).IsValid() {
			return
		}

		ctx, _ := otel.Tracer(gormTracerName).Start(
			db.Statement.Context,
			spanName(operation, db.Statement.Table),
			trace.WithSpanKind(trace.SpanKindClient),
		)
		db.Statement.Context = ctx
	}
}

func endSpan(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Context == nil {
		return
	}

	span := trace.SpanFromContext(db.Statement.Context)
	if !span.IsRecording() {
		return
	}
	defer span.End()

	span.SetAttributes(
		attribute.String("db.system", db.Dialector.Name()),
		// SQL only, never db.Statement.Vars: the parameters carry emails,
		// refresh tokens and password hashes.
		attribute.String("db.statement", db.Statement.SQL.String()),
		attribute.String("db.sql.table", db.Statement.Table),
		attribute.Int64("db.rows_affected", db.Statement.RowsAffected),
	)

	// A missing row is an ordinary outcome that the repositories translate to
	// (nil, nil); marking it as a failure would make every lookup miss look
	// like an incident.
	if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		span.RecordError(db.Error)
		span.SetStatus(codes.Error, db.Error.Error())
	}
}

func spanName(operation, table string) string {
	if table == "" {
		return "gorm." + operation
	}
	return "gorm." + operation + " " + table
}
