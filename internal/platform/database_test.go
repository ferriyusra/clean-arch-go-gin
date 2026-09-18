package platform_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// sqliteConfig points the database at a throwaway file inside the test's own
// temporary directory.
func sqliteConfig(t *testing.T) *platform.Config {
	t.Helper()

	t.Setenv("DEV_MODE", "true")
	t.Setenv("DATABASE_TYPE", "sqlite")
	t.Setenv("DATABASE_DSN", filepath.Join(t.TempDir(), "test.db"))

	return platform.NewConfig()
}

func TestInitializeDatabaseOpensAndPings(t *testing.T) {
	db, err := platform.InitializeDatabase(sqliteConfig(t))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = platform.CloseDatabase(db) })

	// InitializeDatabase already pinged; this proves the handle is usable.
	testutil.NoError(t, platform.PingCheck(db)(context.Background()))
}

// TestInitializeDatabaseRejectsAnUnknownType covers the silent-data-loss case:
// a typo used to fall through to sqlite and write production data to a file.
func TestInitializeDatabaseRejectsAnUnknownType(t *testing.T) {
	cfg := sqliteConfig(t)
	cfg.Database.Type = "postgresql"

	_, err := platform.InitializeDatabase(cfg)

	testutil.Error(t, err, "an unsupported database type")
}

func TestCloseDatabaseIsSafeOnNil(t *testing.T) {
	testutil.NoError(t, platform.CloseDatabase(nil))
}

func TestPingCheckReportsAnUninitializedDatabase(t *testing.T) {
	err := platform.PingCheck(nil)(context.Background())

	testutil.Error(t, err, "ping against a nil database")
}

func TestPingCheckFailsOnceTheDatabaseIsClosed(t *testing.T) {
	// This is what makes the readiness probe meaningful: it has to notice.
	db, err := platform.InitializeDatabase(sqliteConfig(t))
	testutil.NoError(t, err)

	testutil.NoError(t, platform.CloseDatabase(db))

	testutil.Error(t, platform.PingCheck(db)(context.Background()), "ping after close")
}

func TestMigrateCreatesEveryTableAndIsIdempotent(t *testing.T) {
	db, err := platform.InitializeDatabase(sqliteConfig(t))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = platform.CloseDatabase(db) })

	testutil.NoError(t, platform.Migrate(db))

	for _, model := range []any{
		&entity.UserEntity{},
		&entity.RefreshTokenEntity{},
		&entity.CounterEntity{},
		&entity.MessageEntity{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Errorf("expected a table for %T", model)
		}
	}

	// Running again must not duplicate the seeded rows: the container calls
	// Migrate on every boot.
	testutil.NoError(t, platform.Migrate(db))

	var counters, messages int64
	testutil.NoError(t, db.Model(&entity.CounterEntity{}).Count(&counters).Error)
	testutil.NoError(t, db.Model(&entity.MessageEntity{}).Count(&messages).Error)

	testutil.Equal(t, counters, int64(1), "seeded counter rows")
	testutil.Equal(t, messages, int64(1), "seeded message rows")
}
