package content_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestDashboardArticleListFiltersPaginatesAndOrdersByUpdatedAt(t *testing.T) {
	handler, _, notifier := adminServer(t)
	type page struct {
		Articles []struct {
			ID int64 `json:"id"`
		} `json:"articles"`
		Total      int64   `json:"total"`
		Page       float64 `json:"page"`
		TotalPages int64   `json:"totalPages"`
	}
	// Expected values were recorded from the Node server on the same seed.
	for _, tt := range []struct {
		target       string
		ids          []int64
		total, pages int64
		pageNumber   float64
	}{
		{"/articles", []int64{301, 302, 303, 305}, 4, 1, 1},
		{"/articles?status=draft", []int64{303}, 1, 1, 1},
		{"/articles?status=zzz&limit=2&page=2", []int64{303, 305}, 4, 2, 2},
		{"/articles?search=excerpt", []int64{301, 302, 303}, 3, 1, 1},
		{"/articles?search=synthetic%20contract", nil, 0, 0, 1},
		{"/articles?category=synthetic-code&limit=0&page=-3", []int64{302}, 1, 1, 1},
		{"/articles?limit=999", []int64{301, 302, 303, 305}, 4, 1, 1},
	} {
		response := adminRequest(t, handler, notifier, http.MethodGet, tt.target, "")
		var got page
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &got) != nil {
			t.Fatalf("GET %s = %d %s", tt.target, response.Code, response.Body.String())
		}
		ids := make([]int64, 0)
		for _, article := range got.Articles {
			ids = append(ids, article.ID)
		}
		if tt.ids == nil {
			tt.ids = []int64{}
		}
		if !reflect.DeepEqual(ids, tt.ids) || got.Total != tt.total || got.TotalPages != tt.pages || got.Page != tt.pageNumber {
			t.Errorf("GET %s = ids %v total %d page %v pages %d", tt.target, ids, got.Total, got.Page, got.TotalPages)
		}
	}
	empty := adminRequest(t, handler, notifier, http.MethodGet, "/articles?search=nothing-matches", "")
	expectBody(t, empty, http.StatusOK, `{"articles":[],"total":0,"page":1,"totalPages":0}`)
}

func TestDashboardArticleGetReturnsAnyStatusOr404(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodGet, "/articles/303", "")
	expectFields(t, articleFields(t, response), map[string]any{"id": float64(303), "status": "draft", "published_at": nil})
	for _, target := range []string{"/articles/999", "/articles/abc", "/articles/303abc"} {
		expectBody(t, adminRequest(t, handler, notifier, http.MethodGet, target, ""), http.StatusNotFound, `{"error":"Article not found"}`)
	}
}

func TestDashboardCategoryListCountsArticlesInNameOrder(t *testing.T) {
	handler, _, notifier := adminServer(t)
	expectBody(t, adminRequest(t, handler, notifier, http.MethodGet, "/categories", ""), http.StatusOK,
		`{"categories":[`+
			`{"id":104,"name":"Deals","slug":"deals","description":"Promotions","color":"#999999","article_count":0},`+
			`{"id":101,"name":"Synthetic AI","slug":"synthetic-ai","description":"Synthetic artificial intelligence fixtures","color":"#111111","article_count":2},`+
			`{"id":102,"name":"Synthetic Code","slug":"synthetic-code","description":"Synthetic programming fixtures","color":"#222222","article_count":1},`+
			`{"id":103,"name":"Synthetic Startups","slug":"synthetic-startups","description":"Synthetic startup fixtures","color":"#333333","article_count":1}]}`)
}

func TestDashboardReadsHideDatabaseErrors(t *testing.T) {
	handler, db, notifier := adminServer(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/articles", "/articles/301", "/categories"} {
		expectBody(t, adminRequest(t, handler, notifier, http.MethodGet, target, ""), http.StatusInternalServerError, internalError)
	}
}
