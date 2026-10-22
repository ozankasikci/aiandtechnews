package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestArgumentsFailBeforeOpeningDatabaseOrCallingModel(t *testing.T) {
	for _, args := range [][]string{nil, {"-db", "unused.db", "-latest", "0"}, {"-db", "unused.db", "-latest", "2", "-slug", "story"}, {"-db", "unused.db", "extra"}} {
		if err := run(context.Background(), args, io.Discard, func(string) string { return "synthetic-key" }); err == nil || !strings.Contains(err.Error(), "use -db PATH") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
	if err := run(context.Background(), []string{"-db", "unused.db"}, io.Discard, func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY is required") {
		t.Fatalf("err=%v", err)
	}
}
