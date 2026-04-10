package sqlite

import (
	"database/sql"
	"stripeflow/repository"

	"github.com/stephenafamo/bob"
)

type Repository struct {
	db bob.DB
}

func New(db *sql.DB) repository.Querier {
	return &Repository{
		db: bob.NewDB(db),
	}
}
