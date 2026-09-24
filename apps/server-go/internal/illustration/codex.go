package illustration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// DefaultCodexTimeout bounds one codex exec run. Image generations take
// about 60-150s.
const DefaultCodexTimeout = 4 * time.Minute

// CodexReloginHint is what an editor must run on the Mac mini when the
// ChatGPT login behind the Codex CLI has expired.
const CodexReloginHint = "Codex needs re-login: on the Mac mini run `~/aiandtechnews/tools/codex/node_modules/.bin/codex login --device-auth`"

// ErrCodexAuth means the Codex CLI is not logged in or its token could not
// be refreshed. It is a system fault: no article is to blame.
var ErrCodexAuth = errors.New("codex is not logged in")

// CodexRunner runs the Codex CLI non-interactively with the ChatGPT-plan
// login in ~/.codex (never an API key).
type CodexRunner struct {
	// Bin is CODEX_BIN, e.g. ~/aiandtechnews/tools/codex/node_modules/.bin/codex.
	Bin string
	// NodeDir is CODEX_NODE_DIR: a directory holding the node binary the
	// codex.js launcher needs. It is put first on PATH. Empty keeps PATH.
	NodeDir string
	// Home is the user's home, so ~/.codex/auth.json is found. Empty uses $HOME.
	Home    string
	Timeout time.Duration
}

// CodexRun is one codex exec invocation.
type CodexRun struct {
	Dir     string // -C, also the only writable directory in workspace-write
	Sandbox string // read-only or workspace-write
	Images  []string
	Prompt  string
	Extra   []string // extra flags placed before the images
}

// Args returns the exact codex exec argument list. "--" separates the
// repeatable --image flag from the prompt; without it --image swallows the
// prompt.
func (r CodexRun) Args() []string {
	args := []string{"exec", "--skip-git-repo-check", "--sandbox", r.Sandbox, "--color", "never", "-C", r.Dir}
	args = append(args, r.Extra...)
	for _, image := range r.Images {
		args = append(args, "--image="+image)
	}
	return append(args, "--", r.Prompt)
}

// Run executes codex and returns its combined output. A login problem is
// reported as ErrCodexAuth.
func (c *CodexRunner) Run(ctx context.Context, run CodexRun) (string, error) {
	if c.Bin == "" {
		return "", errors.New("CODEX_BIN is not set")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultCodexTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, run.Args()...)
	cmd.Dir = run.Dir
	cmd.Env = c.env()
	cmd.Stdin = nil // /dev/null
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	// codex.js spawns the native binary; kill the whole group on timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	text := output.String()
	if isCodexAuthFailure(text) {
		return text, fmt.Errorf("%w (%s): %s", ErrCodexAuth, CodexReloginHint, tail(text, 300))
	}
	if ctx.Err() != nil {
		return text, fmt.Errorf("codex exec: %w", ctx.Err())
	}
	if err != nil {
		return text, fmt.Errorf("codex exec: %w: %s", err, tail(text, 300))
	}
	return text, nil
}

func (c *CodexRunner) env() []string {
	home := c.Home
	if home == "" {
		home = os.Getenv("HOME")
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	if c.NodeDir != "" {
		path = c.NodeDir + string(os.PathListSeparator) + path
	}
	env := []string{"HOME=" + home, "PATH=" + path, "NO_COLOR=1"}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "PATH", "NO_COLOR":
			continue
		case "OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL":
			// Codex must use the ChatGPT-plan login, never an API key.
			continue
		}
		if strings.HasPrefix(name, "GEMINI_") || strings.HasPrefix(name, "AWS_") || strings.HasSuffix(name, "_SECRET") || strings.HasSuffix(name, "_API_KEY") {
			// The agent runs shell commands; keep the server's secrets from it.
			continue
		}
		env = append(env, entry)
	}
	return env
}

var codexAuthFailure = regexp.MustCompile(`(?i)(not logged in|token refresh failed|failed to refresh (the )?(access )?token|refresh token (was )?(already used|expired|revoked|invalid)|please (re-?)?(log|sign) ?in|codex login|\b401\b[^\n]{0,80}unauthori[sz]ed|unauthori[sz]ed[^\n]{0,80}\b401\b)`)

func isCodexAuthFailure(output string) bool { return codexAuthFailure.MatchString(output) }

// generatedImagePath finds an image Codex left in ~/.codex/generated_images
// when it did not copy it to the requested path.
var generatedImagePath = regexp.MustCompile(`(/[^\s'"` + "`" + `]*/\.codex/generated_images/[^\s'"` + "`" + `]+\.png)`)

func findGeneratedImage(output, home string) string {
	matches := generatedImagePath.FindAllString(output, -1)
	root := filepath.Join(home, ".codex", "generated_images") + string(filepath.Separator)
	for i := len(matches) - 1; i >= 0; i-- {
		candidate := filepath.Clean(matches[i])
		if strings.HasPrefix(candidate, root) {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
	}
	return ""
}

func tail(text string, n int) string {
	text = strings.TrimSpace(text)
	if len(text) <= n {
		return text
	}
	return "…" + text[len(text)-n:]
}
