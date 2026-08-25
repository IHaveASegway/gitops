package clone

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// Options controls how repositories are cloned.
type Options struct {
	Host     string
	Protocol string // "ssh" or "https"
	Token    string // used for HTTPS clones; never written to disk
}

// env authenticates HTTPS clones with the token via a process-scoped git
// config override, so the token is neither persisted in .git/config nor
// visible in the process list.
func env(o Options) []string {
	if o.Protocol != "https" || o.Token == "" {
		return nil
	}
	auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + o.Token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://" + o.Host + "/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + auth,
	}
}

// cleanError reduces git clone's stderr to its meaningful line.
func cleanError(msg string) string {
	var keep []string
	for _, l := range strings.Split(msg, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "Cloning into") {
			continue
		}
		keep = append(keep, l)
	}
	for _, l := range keep {
		if strings.HasPrefix(l, "fatal:") || strings.HasPrefix(l, "ERROR:") || strings.HasPrefix(l, "error:") {
			return l
		}
	}
	if len(keep) == 0 {
		return strings.TrimSpace(msg)
	}
	return keep[len(keep)-1]
}

// Op returns a runner.Func that clones the entry named by its target.
func Op(entries map[string]Entry, o Options) runner.Func {
	return func(ctx context.Context, name string) runner.Result {
		e, ok := entries[name]
		if !ok {
			return runner.Result{Repo: name, Error: "not in plan"}
		}
		if pathExists(e.Dest) {
			return runner.Result{Repo: name, Error: "destination already exists"}
		}
		parent := filepath.Dir(e.Dest)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return runner.Result{Repo: name, Error: "mkdir: " + err.Error()}
		}
		url := e.Repo.RemoteURL(o.Host, o.Protocol)
		if _, err := git.RunEnv(ctx, parent, env(o), "clone", "--", url, e.Dest); err != nil {
			// git removes the directory on ordinary failures; a killed clone
			// (cancellation) leaves a partial checkout behind.
			_ = os.RemoveAll(e.Dest)
			return runner.Result{Repo: name, Error: "clone: " + cleanError(err.Error())}
		}
		msg := "cloned"
		if e.Repo.DefaultBranch != "" {
			msg += " (" + e.Repo.DefaultBranch + ")"
		}
		return runner.Result{Repo: name, Success: true, Output: msg}
	}
}

// Run clones the given entries with at most jobs clones in flight.
func Run(ctx context.Context, entries []Entry, o Options, jobs int, onEvent func(runner.Event)) []runner.Result {
	names := make([]string, len(entries))
	byName := make(map[string]Entry, len(entries))
	for i, e := range entries {
		names[i] = e.Repo.Name
		byName[e.Repo.Name] = e
	}
	return runner.Run(ctx, names, Op(byName, o), jobs, onEvent)
}
