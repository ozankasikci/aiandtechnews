// Package codextext generates JSON text with codex exec (ChatGPT plan
// login). The publisher uses it to write original reports from primary
// documents, where the writing quality matters more than speed.
package codextext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

// DefaultModel, DefaultEffort and DefaultTimeout configure the writer.
const (
	DefaultModel   = "gpt-6-sol"
	DefaultEffort  = "medium"
	DefaultTimeout = 8 * time.Minute
)

// Writer implements the publisher's TextGenerator.
type Writer struct {
	Runner *illustration.CodexRunner
	Model  string
	Effort string
}

// New returns a writer using the given runner.
func New(runner *illustration.CodexRunner, model, effort string) *Writer {
	if model == "" {
		model = DefaultModel
	}
	if effort == "" {
		effort = DefaultEffort
	}
	return &Writer{Runner: runner, Model: model, Effort: effort}
}

// GenerateJSON runs the prompt and returns the model's final message.
func (w *Writer) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	dir, err := os.MkdirTemp("", "codextext-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	answerPath := filepath.Join(dir, "answer.txt")
	output, err := w.Runner.Run(ctx, illustration.CodexRun{
		Dir:     dir,
		Sandbox: "read-only",
		Extra:   []string{"-m", w.Model, "-c", "model_reasoning_effort=" + w.Effort, "-o", answerPath},
		Prompt:  prompt + "\n\nDo not run any commands. Answer with the JSON object only.",
	})
	if err != nil {
		return "", err
	}
	if raw, readErr := os.ReadFile(answerPath); readErr == nil && strings.TrimSpace(string(raw)) != "" {
		return string(raw), nil
	}
	return output, nil
}
