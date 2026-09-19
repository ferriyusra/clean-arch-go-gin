package platform

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// Migration is one ordered, forward-only change to the schema or its data.
//
// Up receives the transaction the migration runs in, so every statement it
// makes is part of the same unit of work as the ledger row that records it.
type Migration struct {
	Version int64
	Name    string
	Up      func(tx *gorm.DB) error
}

// schemaMigration is the ledger. Version is the primary key, which is what
// makes applying the same migration twice impossible rather than merely
// unlikely.
type schemaMigration struct {
	Version   int64 `gorm:"primaryKey"`
	Name      string
	AppliedAt time.Time
}

func (schemaMigration) TableName() string { return "schema_migrations" }

// entities lists every table the application owns.
func entities() []any {
	return []any{
		&entity.UserEntity{},
		&entity.RefreshTokenEntity{},
		&entity.CounterEntity{},
		&entity.MessageEntity{},
	}
}

// migrations is the ordered history of the schema.
//
// Append only, never edit or renumber: a version that has already run
// somewhere is a fact, and changing it means that database and a fresh one no
// longer agree. There are deliberately no down migrations. Reversing a schema
// change in production is nearly always a restore or a new forward migration,
// and a down step that is never exercised is a false sense of safety.
func migrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "create initial schema",
			// AutoMigrate is fine for creating tables and for additive changes.
			// Anything destructive, like migration 3 below, has to be written
			// out, which is exactly the boundary this ledger exists to manage.
			Up: func(tx *gorm.DB) error { return tx.AutoMigrate(entities()...) },
		},
		{
			Version: 2,
			Name:    "seed demo rows",
			Up:      seed,
		},
		{
			Version: 3,
			Name:    "drop the plaintext refresh token column",
			Up:      dropLegacyRefreshTokenColumn,
		},
	}
}

// Migrate applies every migration that has not run yet, in order.
//
// Each one runs in its own transaction together with its ledger row, so a
// failure leaves the database on the last version that fully succeeded rather
// than halfway through a change.
//
// Concurrency is not coordinated: two instances booting against an empty
// database at the same moment will both try, and the loser fails on the
// ledger primary key and exits. That is safe but noisy, which is why
// DATABASE_AUTO_MIGRATE exists — in production, run migrations as their own
// step and start the application with it switched off.
func Migrate(db *gorm.DB) error {
	pending := migrations()
	if err := validateMigrations(pending); err != nil {
		return err
	}

	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		return fmt.Errorf("creating migration ledger: %w", err)
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, migration := range pending {
		if applied[migration.Version] {
			continue
		}

		if err := applyMigration(db, migration); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(db *gorm.DB, migration Migration) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := migration.Up(tx); err != nil {
			return err
		}

		return tx.Create(&schemaMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: time.Now(),
		}).Error
	})
	if err != nil {
		return fmt.Errorf("applying migration %d (%s): %w", migration.Version, migration.Name, err)
	}

	return nil
}

// validateMigrations catches the mistake two branches make when they both add
// "the next" migration and are merged: duplicate or unordered versions. Finding
// it at startup is far better than finding it when one of them silently never
// runs.
func validateMigrations(list []Migration) error {
	sorted := make([]Migration, len(list))
	copy(sorted, list)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	for i, migration := range sorted {
		if migration.Version <= 0 {
			return fmt.Errorf("migration %q has version %d; versions start at 1",
				migration.Name, migration.Version)
		}
		if migration.Up == nil {
			return fmt.Errorf("migration %d (%s) has no Up function", migration.Version, migration.Name)
		}
		if i > 0 && sorted[i-1].Version == migration.Version {
			return fmt.Errorf("duplicate migration version %d (%q and %q)",
				migration.Version, sorted[i-1].Name, migration.Name)
		}
		if list[i].Version != migration.Version {
			return fmt.Errorf("migrations are out of order at version %d (%s); list them ascending",
				migration.Version, migration.Name)
		}
	}

	return nil
}

func appliedVersions(db *gorm.DB) (map[int64]bool, error) {
	var rows []schemaMigration
	if err := db.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading migration ledger: %w", err)
	}

	applied := make(map[int64]bool, len(rows))
	for _, row := range rows {
		applied[row.Version] = true
	}
	return applied, nil
}

// dropLegacyRefreshTokenColumn removes the column that held refresh tokens in
// the clear before they were stored as digests.
//
// AutoMigrate added token_hash and left token untouched, so without this the
// old tokens stay readable in the table and the security fix is only half done.
//
// It goes through raw SQL rather than Migrator().DropColumn on purpose.
// GORM resolves a column name against the model's fields, and RefreshTokenEntity
// no longer has a Token field, so DropColumn reports success and changes
// nothing. The same reason rules out Migrator().HasColumn for the check, so the
// column list is read back from the database instead.
func dropLegacyRefreshTokenColumn(tx *gorm.DB) error {
	const (
		table  = "refresh_token_entities"
		column = "token"
	)

	present, err := hasColumn(tx, &entity.RefreshTokenEntity{}, column)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	// Both sqlite (3.35+) and postgres support DROP COLUMN; the identifiers are
	// constants here, not input.
	if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, column)).Error; err != nil {
		return fmt.Errorf("dropping legacy %s.%s: %w", table, column, err)
	}

	return nil
}

// hasColumn asks the database what columns a table actually has, rather than
// asking GORM what the Go struct says it should have.
func hasColumn(tx *gorm.DB, model any, column string) (bool, error) {
	types, err := tx.Migrator().ColumnTypes(model)
	if err != nil {
		return false, fmt.Errorf("reading columns: %w", err)
	}

	for _, columnType := range types {
		if strings.EqualFold(columnType.Name(), column) {
			return true, nil
		}
	}
	return false, nil
}

// seed inserts the singleton rows the counter and message endpoints read. It is
// idempotent, so re-running it against a populated table changes nothing.
func seed(tx *gorm.DB) error {
	var counters int64
	if err := tx.Model(&entity.CounterEntity{}).Count(&counters).Error; err != nil {
		return fmt.Errorf("counting counters: %w", err)
	}
	if counters == 0 {
		if err := tx.Create(&entity.CounterEntity{ID: uuid.New(), Value: 0}).Error; err != nil {
			return fmt.Errorf("seeding counter: %w", err)
		}
	}

	var messages int64
	if err := tx.Model(&entity.MessageEntity{}).Count(&messages).Error; err != nil {
		return fmt.Errorf("counting messages: %w", err)
	}
	if messages == 0 {
		msg := &entity.MessageEntity{
			ID:    uuid.New(),
			Key:   "default",
			Value: "Welcome to Clean Go Vite React!",
		}
		if err := tx.Create(msg).Error; err != nil {
			return fmt.Errorf("seeding message: %w", err)
		}
	}

	return nil
}
