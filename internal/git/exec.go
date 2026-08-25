// Package git runs git commands and inspects repositories on disk.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Env returns the environment for git subprocesses. Credential prompts are
// disabled (they would hang a parallel run) and messages are forced to
// English so they can be recognized.
func Env(extra ...string) []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return append(env, extra...)
}

// Run executes git in dir and returns its trimmed stdout. On failure the
// returned error carries git's stderr; a canceled context yields "canceled".
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	return RunEnv(ctx, dir, nil, args...)
}

// RunEnv is Run with additional environment variables.
func RunEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = Env(extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		if ctx.Err() != nil {
			return output, errors.New("canceled")
		}
		errOut := strings.TrimSpace(stderr.String())
		if errOut == "" {
			errOut = err.Error()
		}
		return output, errors.New(errOut)
	}
	return output, nil
}

// CheckBranchName reports an error when name is not a legal branch name.
func CheckBranchName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Run(ctx, "", "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid branch name", name)
	}
	return nil
}
