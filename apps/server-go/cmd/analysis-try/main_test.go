package main

import (
	"bytes"

	"context"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/analysis"
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

func TestPrintEvaluationShowsBothDraftsAndVerdicts(t *testing.T) {
	attempts := []analysis.Attempt{
		{Draft: analysis.Draft{Kind: "context", Text: "Rejected first draft."}, Verdict: &analysis.Verdict{OK: false, Reason: "Unsupported strategy claim"}},
		{Draft: analysis.Draft{Kind: "watch", Text: "Corrected second draft."}, Verdict: &analysis.Verdict{OK: true, Reason: "Stated milestone"}},
	}
	var out bytes.Buffer
	printEvaluation(&out, attempts, analysis.Result{Status: "ready", Draft: attempts[1].Draft})
	last := -1
	for _, want := range []string{"Draft 1:", "Rejected first draft.", "Checker 1: ok=false reason=Unsupported strategy claim", "Draft 2:", "Corrected second draft.", "Checker 2: ok=true reason=Stated milestone", "Final: ready"} {
		index := strings.Index(out.String(), want)
		if index <= last {
			t.Fatalf("missing or out-of-order %q: %s", want, out.String())
		}
		last = index
	}
}

func TestPrintEvaluationShowsSkippedRevisionWithoutInventingVerdict(t *testing.T) {
	var out bytes.Buffer
	printEvaluation(&out, []analysis.Attempt{
		{Draft: analysis.Draft{Text: "Rejected draft"}, Verdict: &analysis.Verdict{Reason: "Unsupported"}},
		{Draft: analysis.Draft{Skip: true}},
	}, analysis.Result{Status: "skipped", Reason: "writer skipped"})
	for _, want := range []string{"Draft 1:", "Checker 1: ok=false reason=Unsupported", "Draft 2:", "Skip:true", "Checker 2: not run or no valid verdict", "Final: skipped writer skipped"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
