// Copyright 2021 FerretDB Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pgdb

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgtype/pgxtype"
	"github.com/jackc/pgx/v4"

	"github.com/FerretDB/FerretDB/internal/util/lazyerrors"
)

// Databases returns a sorted list of FerretDB database names / PostgreSQL schema names.
func Databases(ctx context.Context, querier pgxtype.Querier) ([]string, error) {
	sql := "SELECT schema_name FROM information_schema.schemata ORDER BY schema_name"
	rows, err := querier.Query(ctx, sql)
	if err != nil {
		return nil, lazyerrors.Error(err)
	}
	defer rows.Close()

	res := make([]string, 0, 2)
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, lazyerrors.Error(err)
		}

		if strings.HasPrefix(name, "pg_") || name == "information_schema" {
			continue
		}

		res = append(res, name)
	}
	if err = rows.Err(); err != nil {
		return nil, lazyerrors.Error(err)
	}

	return res, nil
}

// CreateDatabase creates a new FerretDB database (PostgreSQL schema).
//
// It returns (possibly wrapped):
//
//   - ErrAlreadyExist if schema already exist.
//   - ErrInvalidDatabaseName if db name doesn't comply with the rules.
//
// Use errors.Is to check the error.
func CreateDatabase(ctx context.Context, querier pgxtype.Querier, db string) error {
	if !validateDatabaseNameRe.MatchString(db) ||
		strings.HasPrefix(db, reservedPrefix) {
		return ErrInvalidDatabaseName
	}

	_, err := querier.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{db}.Sanitize())
	if err == nil {
		err = createSettingsTable(ctx, querier, db)
	}

	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return lazyerrors.Error(err)
	}

	switch pgErr.Code {
	case pgerrcode.DuplicateSchema:
		return ErrAlreadyExist
	case pgerrcode.UniqueViolation, pgerrcode.DuplicateObject:
		// https://www.postgresql.org/message-id/CA+TgmoZAdYVtwBfp1FL2sMZbiHCWT4UPrzRLNnX1Nb30Ku3-gg@mail.gmail.com
		// The same thing for schemas. Reproducible by integration tests.
		return ErrAlreadyExist
	default:
		return lazyerrors.Error(err)
	}
}

// CreateDatabaseIfNotExists creates a new FerretDB database (PostgreSQL schema).
// If the schema already exists, no error is returned.
func CreateDatabaseIfNotExists(ctx context.Context, querier pgxtype.Querier, db string) error {
	if !validateDatabaseNameRe.MatchString(db) ||
		strings.HasPrefix(db, reservedPrefix) {
		return ErrInvalidDatabaseName
	}

	_, err := querier.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{db}.Sanitize())
	if err == nil {
		err = createSettingsTable(ctx, querier, db)
	}

	if err == nil || errors.Is(err, ErrAlreadyExist) {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return lazyerrors.Error(err)
	}

	switch pgErr.Code {
	case pgerrcode.DuplicateSchema:
		return ErrAlreadyExist
	case pgerrcode.UniqueViolation, pgerrcode.DuplicateObject:
		// https://www.postgresql.org/message-id/CA+TgmoZAdYVtwBfp1FL2sMZbiHCWT4UPrzRLNnX1Nb30Ku3-gg@mail.gmail.com
		// The same thing for schemas. Reproducible by integration tests.
		return ErrAlreadyExist
	default:
		return lazyerrors.Error(err)
	}
}

// DropDatabase drops FerretDB database.
//
// It returns ErrSchemaNotExist if schema does not exist.
func DropDatabase(ctx context.Context, querier pgxtype.Querier, db string) error {
	_, err := querier.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{db}.Sanitize()+` CASCADE`)
	if err == nil {
		return nil
	}

	pgErr, ok := err.(*pgconn.PgError)
	if !ok {
		return lazyerrors.Error(err)
	}

	switch pgErr.Code {
	case pgerrcode.InvalidSchemaName:
		return ErrSchemaNotExist
	default:
		return lazyerrors.Error(err)
	}
}
