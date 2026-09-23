package adopt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// Reference is the schema the Go migrations create, with the objects each
// migration introduces.
type Reference struct {
	Schema     *Schema
	Migrations []ReferenceMigration
}

// ReferenceMigration lists the schema objects (keys "table name",
// "index name", ...) a migration creates.
type ReferenceMigration struct {
	Descriptor migrate.Descriptor
	Objects    []string
}

// BuildReference applies descriptors one at a time to a scratch database in
// dir (a new temporary directory when dir is empty, removed afterwards) and
// records which objects each creates. A migration that creates no object, or
// that changes an object an earlier migration created, is refused: the
// verifier could not tell whether its effect is present.
func BuildReference(ctx context.Context, descriptors []migrate.Descriptor, dir string) (reference *Reference, err error) {
	if dir == "" {
		dir, err = os.MkdirTemp("", "technews-adopt-reference-")
		if err != nil {
			return nil, fmt.Errorf("create reference directory: %w", err)
		}
		defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	}
	path := filepath.Join(dir, "reference.db")
	if _, statErr := os.Lstat(path); statErr == nil {
		return nil, fmt.Errorf("reference database %s already exists", path)
	}
	db, err := database.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open reference database: %w", err)
	}
	defer func() {
		err = errors.Join(err, db.Close())
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if removeErr := os.Remove(path + suffix); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()

	reference = &Reference{}
	before := map[string]string{}
	for _, descriptor := range descriptors {
		if _, err := db.ExecContext(ctx, descriptor.SQL); err != nil {
			return nil, fmt.Errorf("apply migration %d (%s) to the reference: %w", descriptor.Version, descriptor.Name, err)
		}
		schema, err := Inspect(ctx, db)
		if err != nil {
			return nil, err
		}
		migration := ReferenceMigration{Descriptor: descriptor}
		for key, object := range schema.Objects {
			previous, existed := before[key]
			switch {
			case !existed:
				migration.Objects = append(migration.Objects, key)
			case previous != object.SQL:
				return nil, fmt.Errorf("migration %d (%s) changes %s created by an earlier migration; adoption cannot verify it", descriptor.Version, descriptor.Name, key)
			}
			before[key] = object.SQL
		}
		if len(migration.Objects) == 0 {
			return nil, fmt.Errorf("migration %d (%s) creates no schema object; adoption cannot tell whether it is present", descriptor.Version, descriptor.Name)
		}
		sort.Strings(migration.Objects)
		reference.Migrations = append(reference.Migrations, migration)
		reference.Schema = schema
	}
	if reference.Schema == nil {
		return nil, errors.New("no migrations to build a reference from")
	}
	return reference, nil
}
