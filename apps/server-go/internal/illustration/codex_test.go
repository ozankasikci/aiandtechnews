package illustration_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

// fakeCodex is a shell-script stand-in for the Codex CLI. It records its
// arguments and environment, then acts according to FAKE_CODEX_MODE: write
// the PNG to the path named in the prompt, answer -o with FAKE_BRIEF, leave
// the image only in ~/.codex/generated_images, fail, hang, or report an
// expired login.
const fakeCodex = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_CODEX_LOG"
env > "$FAKE_CODEX_LOG.env"
for last; do :; done
out=$(printf '%s' "$last" | sed -n 's/.*Save the final image as \(.*\.png\) and do nothing else.*/\1/p')
answer=""; prev=""
for a in "$@"; do [ "$prev" = "-o" ] && answer="$a"; prev="$a"; done
case "$FAKE_CODEX_MODE" in
auth) echo "ERROR: token refresh failed: 401 Unauthorized"; exit 1 ;;
fail) echo "something broke"; exit 3 ;;
noimage) echo "I could not do that."; exit 0 ;;
hang) sleep 30; exit 0 ;;
fallback)
  mkdir -p "$HOME/.codex/generated_images/session"
  printf '%s' "$FAKE_PNG_B64" | base64 -d > "$HOME/.codex/generated_images/session/img.png"
  echo "Generated $HOME/.codex/generated_images/session/img.png"
  exit 0 ;;
esac
if [ -n "$answer" ]; then printf '%s' "$FAKE_BRIEF" > "$answer"; echo "{\"ignored\":true}"; exit 0; fi
printf '%s' "$FAKE_PNG_B64" | base64 -d > "$out"
echo "Saved to $out"
`

type codexHarness struct {
	runner *illustration.CodexRunner
	log    string
	home   string
}

func newFakeCodex(t *testing.T, mode string) *codexHarness {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte(fakeCodex), 0o755); err != nil {
		t.Fatal(err)
	}
	nodeDir := filepath.Join(dir, "node", "bin")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "args.log")
	t.Setenv("FAKE_CODEX_MODE", mode)
	t.Setenv("FAKE_CODEX_LOG", log)
	t.Setenv("FAKE_PNG_B64", base64.StdEncoding.EncodeToString(solidPNG(t, 32, 18)))
	t.Setenv("FAKE_BRIEF", validBrief)
	t.Setenv("OPENAI_API_KEY", "sk-must-not-leak")
	t.Setenv("GEMINI_API_KEY", "must-not-leak")
	return &codexHarness{runner: &illustration.CodexRunner{Bin: bin, NodeDir: nodeDir, Home: home, Timeout: 20 * time.Second}, log: log, home: home}
}

func (h *codexHarness) args(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(h.log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func (h *codexHarness) env(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(h.log + ".env")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sampleRequest(t *testing.T) illustration.GenerateRequest {
	t.Helper()
	brief, err := illustration.ParseBrief(validBrief, styleNames)
	if err != nil {
		t.Fatal(err)
	}
	style, _ := styles.MustLoad().Get("gouache")
	return illustration.GenerateRequest{Brief: brief, Style: style}
}

func TestCodexProviderGeneratesWithAnchorsAndLogin(t *testing.T) {
	harness := newFakeCodex(t, "write")
	provider := illustration.NewCodexProvider(harness.runner)
	image, err := provider.Generate(context.Background(), sampleRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(image) == 0 || provider.Attempts() != 2 {
		t.Fatalf("image %d bytes, attempts %d", len(image), provider.Attempts())
	}
	args := harness.args(t)
	for _, want := range []string{"exec", "--skip-git-repo-check", "--sandbox", "workspace-write", "-C"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args lack %q: %q", want, args)
		}
	}
	separator := slices.Index(args, "--")
	if separator < 0 || separator != len(args)-2 {
		t.Fatalf("-- must come right before the prompt: %q", args)
	}
	images := 0
	for _, arg := range args[:separator] {
		if strings.HasPrefix(arg, "--image=") {
			images++
		}
	}
	if images != 3 {
		t.Fatalf("attached %d anchors, want 3", images)
	}
	prompt := args[len(args)-1]
	for _, want := range []string{"$imagegen Create one image.", "Scene: A glowing helix", "Style: gouache painting", illustration.ImageRules, "style references only", "Save the final image as /", "and do nothing else."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q: %s", want, prompt)
		}
	}
	env := harness.env(t)
	if strings.Contains(env, "sk-must-not-leak") || strings.Contains(env, "GEMINI_API_KEY") {
		t.Fatal("codex must not see API keys")
	}
	if !strings.Contains(env, "HOME="+harness.home+"\n") || !strings.Contains(env, "PATH="+harness.runner.NodeDir+":") {
		t.Fatalf("HOME/PATH not set for codex:\n%s", env)
	}
	// The per-run temp dir (with the anchors and the image) is removed.
	workdir := args[slices.Index(args, "-C")+1]
	if _, err := os.Stat(workdir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp dir %s left behind: %v", workdir, err)
	}
}

func TestCodexProviderFindsImageInGeneratedImages(t *testing.T) {
	harness := newFakeCodex(t, "fallback")
	image, err := illustration.NewCodexProvider(harness.runner).Generate(context.Background(), sampleRequest(t))
	if err != nil || len(image) == 0 {
		t.Fatalf("image %d bytes, err %v", len(image), err)
	}
}

func TestCodexProviderFailures(t *testing.T) {
	harness := newFakeCodex(t, "noimage")
	if _, err := illustration.NewCodexProvider(harness.runner).Generate(context.Background(), sampleRequest(t)); !errors.Is(err, illustration.ErrCodexNoImage) {
		t.Fatalf("noimage: err = %v", err)
	}
	harness = newFakeCodex(t, "fail")
	if _, err := illustration.NewCodexProvider(harness.runner).Generate(context.Background(), sampleRequest(t)); err == nil || errors.Is(err, illustration.ErrCodexAuth) {
		t.Fatalf("fail: err = %v", err)
	}
	harness = newFakeCodex(t, "auth")
	_, err := illustration.NewCodexProvider(harness.runner).Generate(context.Background(), sampleRequest(t))
	if !errors.Is(err, illustration.ErrCodexAuth) || !strings.Contains(err.Error(), "codex login --device-auth") {
		t.Fatalf("auth: err = %v", err)
	}
}

func TestCodexRunnerTimeoutKillsTheProcess(t *testing.T) {
	harness := newFakeCodex(t, "hang")
	harness.runner.Timeout = 500 * time.Millisecond
	started := time.Now()
	_, err := harness.runner.Run(context.Background(), illustration.CodexRun{Dir: t.TempDir(), Sandbox: "read-only", Prompt: "x"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestCodexAnalyzerReadsTheAnswerFile(t *testing.T) {
	harness := newFakeCodex(t, "answer")
	analyzer := illustration.NewCodexAnalyzer(harness.runner, styles.MustLoad())
	brief, err := analyzer.Analyze(context.Background(), illustration.AnalyzeInput{Title: "T", Excerpt: "E", Source: tinyPNG(t)})
	if err != nil {
		t.Fatal(err)
	}
	if brief.Style != "gouache" || analyzer.Name() != "codex" {
		t.Fatalf("brief = %+v", brief)
	}
	args := harness.args(t)
	for _, want := range []string{"read-only", "--output-schema", "-o"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args lack %q: %q", want, args)
		}
	}
	separator := slices.Index(args, "--")
	if separator < 1 || !strings.HasPrefix(args[separator-1], "--image=") {
		t.Fatalf("the source image must be attached right before --: %q", args)
	}
}

func TestCodexAuthFailureIsASystemFaultInThePipeline(t *testing.T) {
	harness := newFakeCodex(t, "auth")
	pipeline := illustration.NewPipeline(illustration.PipelineDeps{
		Chain:     []illustration.Step{illustration.ProviderStep(illustration.NewCodexProvider(harness.runner))},
		Analyzers: []illustration.Analyzer{&fakeAnalyzer{name: "gemini", brief: validBrief}},
		Reviewer:  &fakeReviewer{},
		Styles:    styles.MustLoad(),
		Logger:    discardLogger(),
	})
	_, err := pipeline.Produce(context.Background(), publisher.IllustrationRequest{Slug: "s", Title: "T", Excerpt: "E"})
	if !publisher.IsSystemFault(err) || !errors.Is(err, illustration.ErrCodexAuth) {
		t.Fatalf("err = %v, want system fault", err)
	}
}
