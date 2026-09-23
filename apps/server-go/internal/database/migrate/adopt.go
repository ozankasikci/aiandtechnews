package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	// ErrAlreadyManaged reports that Adopt found an existing migration ledger.
	ErrAlreadyManaged = errors.New("database is already managed by the migration ledger")
	// ErrNothingToAdopt reports that Adopt found an empty database; Run
	// migrates those directly.
	ErrNothingToAdopt = errors.New("database is empty; there is nothing to adopt (use Run)")
)

// AdoptResult lists what Adopt did, in version order.
type AdoptResult struct {
	// Recorded are the versions whose schema already existed: they were only
	// written to the ledger, their SQL did not run.
	Recorded []int64
	// Applied are the versions whose SQL ran.
	Applied []int64
}

// Adopt brings an unmanaged database (one created by another program, such
// as the Node server) under the migration ledger in a single BEGIN IMMEDIATE
// transaction:
//
//  1. it refuses a database that already has a ledger (ErrAlreadyManaged) or
//     has no schema at all (ErrNothingToAdopt);
//  2. it calls verify on the transaction's connection, so the verification
//     and the writes see the same schema; verify returns the versions whose
//     schema is already present and must fail when the schema is not
//     compatible;
//  3. it creates the ledger with Run's DDL, records the present versions
//     with their exact names and checksums, and runs every other descriptor
//     in version order.
//
// Any error rolls the whole transaction back, leaving the database as it
// was. Afterwards every descriptor is recorded, so Run finds no gap. Adopt
// never decides compatibility itself; that is verify's job.
func Adopt(ctx context.Context, db *sql.DB, descriptors []Descriptor, verify func(context.Context, Queryer) ([]int64, error)) (result AdoptResult, err error) {
	if err := validateDescriptors(descriptors); err != nil {
		return AdoptResult{}, err
	}
	if ctx == nil {
		return AdoptResult{}, errors.New("adopt database: nil context")
	}
	if db == nil {
		return AdoptResult{}, errors.New("adopt database: nil database")
	}
	if verify == nil {
		return AdoptResult{}, errors.New("adopt database: nil verify function")
	}
	if err := ctx.Err(); err != nil {
		return AdoptResult{}, fmt.Errorf("adopt database: %w", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return AdoptResult{}, fmt.Errorf("adopt database: acquire connection: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return AdoptResult{}, fmt.Errorf("adopt database: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
			defer cancel()
			if _, rollbackErr := conn.ExecContext(rollbackCtx, "ROLLBACK"); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback adoption: %w", rollbackErr))
			}
		}
	}()

	exists, err := ledgerExists(ctx, conn)
	if err != nil {
		return AdoptResult{}, err
	}
	if exists {
		return AdoptResult{}, ErrAlreadyManaged
	}
	empty, err := databaseIsEmpty(ctx, conn)
	if err != nil {
		return AdoptResult{}, err
	}
	if empty {
		return AdoptResult{}, ErrNothingToAdopt
	}

	presentVersions, err := verify(ctx, conn)
	if err != nil {
		return AdoptResult{}, fmt.Errorf("verify schema: %w", err)
	}
	known := make(map[int64]bool, len(descriptors))
	for _, descriptor := range descriptors {
		known[descriptor.Version] = true
	}
	present := make(map[int64]bool, len(presentVersions))
	for _, version := range presentVersions {
		if !known[version] {
			return AdoptResult{}, fmt.Errorf("%w: verify reported unknown version %d", ErrInvalidDescriptors, version)
		}
		if present[version] {
			return AdoptResult{}, fmt.Errorf("%w: verify reported version %d twice", ErrInvalidDescriptors, version)
		}
		present[version] = true
	}

	if _, err := conn.ExecContext(ctx, ledgerSchema); err != nil {
		return AdoptResult{}, fmt.Errorf("create migration ledger: %w", err)
	}
	for _, descriptor := range descriptors {
		if present[descriptor.Version] {
			result.Recorded = append(result.Recorded, descriptor.Version)
		} else {
			if _, err := conn.ExecContext(ctx, descriptor.SQL); err != nil {
				return AdoptResult{}, fmt.Errorf("apply migration %d (%s): %w", descriptor.Version, descriptor.Name, err)
			}
			result.Applied = append(result.Applied, descriptor.Version)
		}
		if err := recordMigration(ctx, conn, descriptor); err != nil {
			return AdoptResult{}, fmt.Errorf("record migration %d (%s): %w", descriptor.Version, descriptor.Name, err)
		}
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return AdoptResult{}, fmt.Errorf("commit adoption: %w", err)
	}
	committed = true
	return result, nil
}
