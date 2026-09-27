package publisher

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type answer struct {
	text  string
	err   error
	calls int
}

func (a *answer) GenerateJSON(context.Context, string) (string, error) {
	a.calls++
	return a.text, a.err
}

func TestAddSubheadingsOnlyFillsArticlesWithoutAny(t *testing.T) {
	body := "<p>1</p><p>2</p><p>3</p><p>4</p><p>5</p><p>6</p>"
	good := &answer{text: `{"subheadings":[{"beforeParagraph":4,"text":"The middle part"}]}`}
	p := &Publisher{Deps: Deps{Subheadings: good}}
	if got := p.addSubheadings(context.Background(), "T", body); got != "<p>1</p><p>2</p><p>3</p><h2>The middle part</h2><p>4</p><p>5</p><p>6</p>" {
		t.Fatalf("got %q", got)
	}
	has := "<p>1</p><h2>A</h2><p>2</p><p>3</p><p>4</p><p>5</p>"
	short := "<p>1</p><p>2</p><p>3</p><p>4</p>"
	calls := good.calls
	if p.addSubheadings(context.Background(), "T", has) != has || p.addSubheadings(context.Background(), "T", short) != short || good.calls != calls {
		t.Fatal("articles with subheadings or under 5 paragraphs must be left alone without a call")
	}
	for _, bad := range []*answer{{err: errors.New("down")}, {text: "nope"}} {
		p := &Publisher{Deps: Deps{Subheadings: bad}}
		if got := p.addSubheadings(context.Background(), "T", body); got != body {
			t.Fatalf("failure must keep the body: %q", got)
		}
	}
	if got := (&Publisher{}).addSubheadings(context.Background(), "T", body); !strings.EqualFold(got, body) {
		t.Fatal("without a model the body is unchanged")
	}
}
