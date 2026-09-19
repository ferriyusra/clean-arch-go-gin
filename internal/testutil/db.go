package testutil

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"
)

// NewDB returns a migrated, isolated, in-memory database for a single test.
//
// Two details are load-bearing and easy to get wrong:
//
//   - A bare ":memory:" DSN gives every *connection* its own empty database, so
//     a migration on one pooled connection is invisible to a query on the next.
//     A named database with cache=shared plus MaxOpenConns(1) keeps every
//     statement on one database and serialises access to it.
//   - The name embeds the test name and a UUID so that tests, including
//     parallel ones, never see each other's rows.
//
// The pragma syntax below is glebarez/modernc's ("_pragma=foreign_keys(1)"),
// not mattn's ("_foreign_keys=1").
func NewDB(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s_%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", name, uuid.NewString())

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		// Silent keeps `go test -v` readable; flip to logger.Info to debug SQL.
		Logger: logger.Default.LogMode(logger.Silent),
		// Matches the production configuration in platform.InitializeDatabase;
		// without it a test could not observe the duplicate-key mapping at all.
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("getting underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := platform.Migrate(db); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}

	return db
}

// MustCreate inserts rows and fails the test if any insert fails.
func MustCreate(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seeding %T: %v", row, err)
		}
	}
}
