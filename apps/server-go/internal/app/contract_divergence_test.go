package app_test

import (
	"encoding/json"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
)

// publicArticleOperations are recorded Node article reads whose responses
// this server deliberately changes: it never publishes an article's source
// or source_url, which stay stored for duplicate detection and the dashboard.
var publicArticleOperations = map[string]bool{
	"articles.list": true, "articles.trending": true, "articles.getBySlug": true, "articles.getById": true,
}

// withoutArticleSource returns op with source and source_url removed from the
// recorded article(s) when op is a public article read, and op unchanged
// otherwise.
func withoutArticleSource(t *testing.T, op contracttest.Operation) contracttest.Operation {
	t.Helper()
	if !publicArticleOperations[op.OperationID] {
		return op
	}
	var body map[string]any
	if err := json.Unmarshal(op.Response.Body, &body); err != nil {
		t.Fatalf("%s body: %v", op.OperationID, err)
	}
	strip := func(article any) {
		if fields, ok := article.(map[string]any); ok {
			delete(fields, "source")
			delete(fields, "source_url")
		}
	}
	if articles, ok := body["articles"].([]any); ok {
		for _, article := range articles {
			strip(article)
		}
	}
	strip(body["article"])
	stripped, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	op.Response.Body = stripped
	return op
}
