package platform

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// This is an internal test so that validateMigrations and the migration list
// itself can be exercised; those are the parts a mistake actually lands in.

func newMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	return db
}

func TestMigrateAppliesEveryMigrationOnce(t *testing.T) {
	db := newMigrationDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("first run: %v", err)
	}

	var ledger []schemaMigration
	if err := db.Order("version").Find(&ledger).Error; err != nil {
		t.Fatalf("reading ledger: %v", err)
	}
	if len(ledger) != len(migrations()) {
		t.Fatalf("expected %d ledger rows, got %d", len(migrations()), len(ledger))
	}

	// Running again must be a no-op. The container migrates on every boot, so
	// this is the common case, not an edge case.
	if err := Migrate(db); err != nil {
		t.Fatalf("second run: %v", err)
	}

	var after int64
	if err := db.Model(&schemaMigration{}).Count(&after).Error; err != nil {
		t.Fatalf("counting ledger: %v", err)
	}
	if after != int64(len(ledger)) {
		t.Errorf("ledger grew on a repeat run: %d then %d", len(ledger), after)
	}

	// And the seed did not double up.
	var counters, messages int64
	_ = db.Model(&entity.CounterEntity{}).Count(&counters).Error
	_ = db.Model(&entity.MessageEntity{}).Count(&messages).Error
	if counters != 1 || messages != 1 {
		t.Errorf("expected one seeded row each, got %d counters and %d messages", counters, messages)
	}
}

func TestMigrateCreatesEveryTable(t *testing.T) {
	db := newMigrationDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	for _, model := range entities() {
		if !db.Migrator().HasTable(model) {
			t.Errorf("expected a table for %T", model)
		}
	}
}

// TestMigrateDropsTheLegacyPlaintextColumn is the reason this ledger exists.
// AutoMigrate is additive, so it added token_hash and left the column holding
// tokens in the clear exactly where it was.
func TestMigrateDropsTheLegacyPlaintextColumn(t *testing.T) {
	db := newMigrationDB(t)

	// Stand up the old shape: the entity as it is now, plus the dropped column.
	if err := db.AutoMigrate(&entity.RefreshTokenEntity{}); err != nil {
		t.Fatalf("creating the old table: %v", err)
	}
	if err := db.Exec("ALTER TABLE refresh_token_entities ADD COLUMN token text").Error; err != nil {
		t.Fatalf("adding the legacy column: %v", err)
	}
	if present, _ := hasColumn(db, &entity.RefreshTokenEntity{}, "token"); !present {
		t.Fatalf("expected the legacy column to exist before migrating")
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	if present, _ := hasColumn(db, &entity.RefreshTokenEntity{}, "token"); present {
		t.Errorf("the plaintext token column must be gone after migrating")
	}
}

// A database that never had the column must migrate cleanly too, which is the
// case for every fresh install.
func TestMigrateIsFineWhenTheLegacyColumnWasNeverThere(t *testing.T) {
	db := newMigrationDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("migrating a fresh database: %v", err)
	}
}

func TestValidateMigrations(t *testing.T) {
	noop := func(*gorm.DB) error { return nil }

	tests := []struct {
		name    string
		list    []Migration
		wantErr string
	}{
		{
			name: "ascending and unique is fine",
			list: []Migration{{1, "a", noop}, {2, "b", noop}},
		},
		{
			// The mistake two branches make when both add "the next" one.
			name:    "duplicate versions",
			list:    []Migration{{1, "a", noop}, {1, "b", noop}},
			wantErr: "duplicate migration version",
		},
		{
			name:    "out of order",
			list:    []Migration{{2, "b", noop}, {1, "a", noop}},
			wantErr: "out of order",
		},
		{
			name:    "version below one",
			list:    []Migration{{0, "a", noop}},
			wantErr: "versions start at 1",
		},
		{
			name:    "missing up function",
			list:    []Migration{{1, "a", nil}},
			wantErr: "no Up function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMigrations(tt.list)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected an error mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected the error to mention %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// The shipped list has to satisfy its own rules, or every boot fails.
func TestShippedMigrationsAreValid(t *testing.T) {
	if err := validateMigrations(migrations()); err != nil {
		t.Errorf("the migration list is invalid: %v", err)
	}
}
