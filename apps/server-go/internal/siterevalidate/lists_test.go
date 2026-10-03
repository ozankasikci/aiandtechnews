package siterevalidate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListsFlagOnlyOnListClients(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
	}))
	defer server.Close()
	plain := NewClient(server.URL, "s", nil)
	lists := NewClient(server.URL, "s", nil)
	lists.lists = true
	if err := plain.Revalidate(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := lists.Revalidate(context.Background(), []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := bodies[0]["lists"]; ok {
		t.Fatalf("an image or summary update must not ask for lists: %v", bodies[0])
	}
	if bodies[1]["lists"] != true {
		t.Fatalf("a publish must ask for lists: %v", bodies[1])
	}
}
