package migrate

import (
	"context"
	"errors"
	"fmt"
)

// Status is the read-only counterpart of Run: it verifies the ledger exactly
// as Run does (shape, unique names, applied_at, names and checksums of the
// applied versions, no unknown or gapped versions) and returns the
// descriptors that are not applied yet. It never writes. A database without
// a ledger is ErrUnmanagedDatabase, whether it is empty or not.
func Status(ctx context.Context, q Queryer, descriptors []Descriptor) ([]Descriptor, error) {
	if err := validateDescriptors(descriptors); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("migration status: nil context")
	}
	if q == nil {
		return nil, errors.New("migration status: nil database")
	}
	exists, err := ledgerExists(ctx, q)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrUnmanagedDatabase
	}
	if err := validateLedger(ctx, q); err != nil {
		return nil, err
	}
	applied, appliedVersions, highest, err := readApplied(ctx, q)
	if err != nil {
		return nil, err
	}
	byVersion := make(map[int64]Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		byVersion[descriptor.Version] = descriptor
	}
	for _, version := range appliedVersions {
		record := applied[version]
		descriptor, ok := byVersion[version]
		if !ok {
			return nil, fmt.Errorf("%w: version %d", ErrUnknownMigration, version)
		}
		if descriptor.Name != record.name || descriptor.Checksum() != record.checksum {
			return nil, fmt.Errorf("%w: version %d", ErrMigrationDrift, version)
		}
	}
	var pending []Descriptor
	for _, descriptor := range descriptors {
		if _, ok := applied[descriptor.Version]; ok {
			continue
		}
		if descriptor.Version < highest {
			return nil, fmt.Errorf("%w: unapplied version %d precedes applied version %d", ErrHistoryGap, descriptor.Version, highest)
		}
		pending = append(pending, descriptor)
	}
	return pending, nil
}
