package counter

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// GORMCounterRepository is a GORM implementation of CounterRepository
type GORMCounterRepository struct {
	db *gorm.DB
}

type CounterModel = entity.CounterEntity

// NewGORMCounterRepository creates a new GORM counter repository.
//
// Schema migration and the seeding of the singleton counter row both live in
// platform.Migrate; see NewGORMUserRepository for why.
func NewGORMCounterRepository(db *gorm.DB) *GORMCounterRepository {
	return &GORMCounterRepository{
		db: db,
	}
}
