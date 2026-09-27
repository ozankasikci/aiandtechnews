package relevance

import (
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
)

func TestParseAnswerMapsIDsToItems(t *testing.T) {
	items := []collector.JudgeItem{{Key: "https://a.test/1"}, {Key: "https://a.test/2"}}
	got, err := parseAnswer([]byte("noise {\"items\":[{\"id\":1,\"candidate\":true,\"reason\":\"AI chips\"},{\"id\":2,\"candidate\":false,\"reason\":\"gadget review\"},{\"id\":9,\"candidate\":true,\"reason\":\"x\"}]} tail"), items)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["https://a.test/1"].Candidate || got["https://a.test/2"].Candidate || got["https://a.test/2"].Reason != "gadget review" {
		t.Fatalf("verdicts = %+v", got)
	}
	if _, err := parseAnswer([]byte("not json"), items); err == nil {
		t.Fatal("want an error for a non-JSON answer")
	}
}
