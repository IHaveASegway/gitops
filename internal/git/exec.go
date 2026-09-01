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
// Use it when a branch is being created; for checking out an arbitrary
// existing ref use CheckRefArg, which is laxer.
func CheckBranchName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Run(ctx, "", "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid branch name", name)
	}
	return nil
}

// CheckRefArg validates a ref passed to a command like checkout. It only
// guards against an empty value and against option injection (a leading
// dash), so any real ref — a tag, remote-tracking branch, SHA, HEAD~1 or
// the @{-1} "previous branch" alias — is accepted; git reports a
// non-existent ref itself. It is the option-injection defense that the "--"
// terminator alone does not provide (git parses a leading-dash ref as a
// flag before reaching the "--").
func CheckRefArg(name string) error {
	if name == "" {
		return errors.New("a branch or ref is required")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q is not a valid branch or ref (must not start with '-')", name)
	}
	return nil
}
