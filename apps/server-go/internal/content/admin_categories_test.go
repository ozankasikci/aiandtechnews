package content_test

import (
	"net/http"
	"testing"
)

// Expected statuses and bodies were recorded from the Node server on the same seed.
func TestDashboardCategoryMutationsMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name, method, target, body string
		status                     int
		want                       string
	}{
		{"create requires name and slug", "POST", "/categories", `{"slug":"x"}`, 400, `{"error":"Name and slug are required"}`},
		{"create applies defaults", "POST", "/categories", `{"name":"New","slug":"new"}`, 201, `{"category":{"id":105,"name":"New","slug":"new","description":"","color":"#6366f1"}}`},
		{"create duplicate slug is a 500", "POST", "/categories", `{"name":"Dup","slug":"synthetic-ai"}`, 500, internalError},
		{"update one field", "PUT", "/categories/101", `{"color":"#000000"}`, 200, `{"category":{"id":101,"name":"Synthetic AI","slug":"synthetic-ai","description":"Synthetic artificial intelligence fixtures","color":"#000000"}}`},
		{"update with no fields", "PUT", "/categories/101", `{}`, 400, `{"error":"No fields to update"}`},
		{"update missing category", "PUT", "/categories/999", `{"name":"x"}`, 404, `{"error":"Category not found"}`},
		{"update null name is a 500", "PUT", "/categories/101", `{"name":null}`, 500, internalError},
		{"update duplicate slug is a 500", "PUT", "/categories/102", `{"slug":"synthetic-ai"}`, 500, internalError},
		{"delete category in use", "DELETE", "/categories/101", ``, 400, `{"error":"Cannot delete category with existing articles. Reassign articles first."}`},
		{"delete missing category", "DELETE", "/categories/999", ``, 404, `{"error":"Category not found"}`},
		{"delete unused category", "DELETE", "/categories/104", ``, 200, `{"success":true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, tt.method, tt.target, tt.body), tt.status, tt.want)
		})
	}
}

func TestDashboardCategoryFailuresLeaveDataUnchanged(t *testing.T) {
	handler, db, notifier := adminServer(t)
	adminRequest(t, handler, notifier, http.MethodPut, "/categories/102", `{"name":"Renamed","slug":"synthetic-ai"}`)
	if renamed := countRows(t, db, `SELECT COUNT(*) FROM categories WHERE name = 'Renamed'`); renamed != 0 {
		t.Errorf("failed update partially applied")
	}
	adminRequest(t, handler, notifier, http.MethodDelete, "/categories/101", "")
	if remaining := countRows(t, db, `SELECT COUNT(*) FROM categories`); remaining != 4 {
		t.Errorf("categories = %d, want 4", remaining)
	}
}
