package content

import (
	"context"
	"fmt"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

// CreateCategory ports dashboard.ts:493-511. A duplicate slug violates the
// UNIQUE constraint and surfaces as a 500, exactly like Node.
func (s *AdminService) CreateCategory(ctx context.Context, body jsonbody.Object) (Category, error) {
	name, slug := body.Get("name"), body.Get("slug")
	if !name.Truthy() || !slug.Truthy() {
		return Category{}, badRequest("Name and slug are required")
	}
	description, color := body.Get("description"), body.Get("color")
	var category Category
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		values, err := bindAll(name.Bind, slug.Bind,
			func() (any, error) { return orEmpty(description) },
			func() (any, error) {
				if color.Truthy() {
					return color.Bind()
				}
				return "#6366f1", nil
			})
		if err != nil {
			return fmt.Errorf("bind category insert: %w", err)
		}
		id, err := tx.insertCategory(ctx, values[0], values[1], values[2], values[3])
		if err != nil {
			return err
		}
		category, err = tx.category(ctx, id)
		return err
	})
	if err != nil {
		return Category{}, err
	}
	return category, nil
}

// UpdateCategory ports dashboard.ts:513-548: every defined property is written,
// so null values and duplicate slugs fail in SQLite and surface as a 500.
func (s *AdminService) UpdateCategory(ctx context.Context, id string, body jsonbody.Object) (Category, error) {
	var category Category
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		exists, err := tx.categoryExists(ctx, id)
		if err != nil {
			return err
		}
		if !exists {
			return categoryNotFound()
		}
		assignments := make([]assignment, 0, 4)
		for _, column := range []string{"name", "slug", "description", "color"} {
			value := body.Get(column)
			if !value.Defined() {
				continue
			}
			bound, err := value.Bind()
			if err != nil {
				return fmt.Errorf("bind %s: %w", column, err)
			}
			assignments = append(assignments, assignment{column, bound})
		}
		if len(assignments) == 0 {
			return badRequest("No fields to update")
		}
		if err := tx.updateRow(ctx, "categories", id, assignments); err != nil {
			return err
		}
		category, err = tx.category(ctx, id)
		return err
	})
	if err != nil {
		return Category{}, err
	}
	return category, nil
}

// DeleteCategory ports dashboard.ts:550-573. The in-use check runs before the
// existence check, so a missing id with no articles is a 404.
func (s *AdminService) DeleteCategory(ctx context.Context, id string) error {
	return s.store.inAdminTx(ctx, func(tx adminTx) error {
		count, err := tx.categoryArticleCount(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return badRequest("Cannot delete category with existing articles. Reassign articles first.")
		}
		deleted, err := tx.deleteRow(ctx, "categories", id)
		if err != nil {
			return err
		}
		if !deleted {
			return categoryNotFound()
		}
		return nil
	})
}
