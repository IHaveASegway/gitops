// Package cli wires the command line: subcommands for each git operation,
// the init command, and the interactive TUI when no subcommand is given.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/urfave/cli/v2"

	"github.com/IHaveASegway/gitops/internal/buildinfo"
	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/ops"
	"github.com/IHaveASegway/gitops/internal/report"
	"github.com/IHaveASegway/gitops/internal/runner"
	"github.com/IHaveASegway/gitops/internal/tui"
)

// Run executes the application with the given os.Args-style arguments.
// Errors that carry an exit code are handled by the framework; any other
// error is returned to the caller.
func Run(args []string) error {
	app := newApp()
	return app.Run(interspersedArgs(app, args))
}

func newApp() *cli.App {
	return &cli.App{
		Name:    "gitops",
		Usage:   "Mass git operations across multiple repositories",
		Version: buildinfo.Version(),
		Flags:   append(sharedFlags(), dirFlag()),
		Action:  runTUI,
		Commands: []*cli.Command{
			repoCommand("pull", "Checkout default branch and pull latest", nil,
				func(*cli.Context) runner.Func { return ops.Pull }),
			repoCommand("sync", "Stash changes, checkout default branch, pull, pop stash", nil,
				func(*cli.Context) runner.Func { return ops.Sync }),
			repoCommand("status", "Show branch, ahead/behind and working tree status for all repos", nil,
				func(*cli.Context) runner.Func { return ops.Status }),
			repoCommand("branch", "Create a new branch from the default branch",
				[]cli.Flag{&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "Name of the new branch", Required: true}},
				func(c *cli.Context) runner.Func { return ops.CreateBranch(c.String("name")) }),
			repoCommand("checkout", "Checkout an existing branch",
				[]cli.Flag{&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "Branch name to checkout", Required: true}},
				func(c *cli.Context) runner.Func { return ops.Checkout(c.String("name")) }),
			repoCommand("push", "Stage all changes, commit, and push the current branch",
				[]cli.Flag{&cli.StringFlag{Name: "message", Aliases: []string{"m"}, Usage: "Commit message", Required: true}},
				func(c *cli.Context) runner.Func { return ops.Push(c.String("message")) }),
			repoCommand("reset", "Discard ALL local changes, force checkout default branch, pull", nil,
				func(*cli.Context) runner.Func { return ops.Reset }),
			initCommand(),
		},
	}
}

func sharedFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "repos", Aliases: []string{"r"}, Usage: "Comma-separated list of repos to operate on"},
		&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Value: runner.DefaultJobs, Usage: "Maximum number of repos processed in parallel"},
	}
}

func dirFlag() cli.Flag {
	return &cli.StringFlag{Name: "dir", Aliases: []string{"d"}, Usage: "Base directory containing repos (default: current directory)"}
}

// repoCommand builds a subcommand that runs one git operation across repos.
func repoCommand(name, usage string, extra []cli.Flag, build func(*cli.Context) runner.Func) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: usage,
		Flags: append(append(sharedFlags(), dirFlag()), extra...),
		Action: func(c *cli.Context) error {
			baseDir, err := resolveBaseDir(c.String("dir"))
			if err != nil {
				return err
			}
			repos, err := resolveRepos(baseDir, c.String("repos"))
			if err != nil {
				return err
			}
			ctx, stop := signalContext()
			defer stop()
			results := runner.Run(ctx, repos, build(c), c.Int("jobs"), nil)
			report.PrintResults(os.Stdout, strings.ToUpper(name[:1])+name[1:]+" results", results)
			return failIfAny(results)
		},
	}
}

// signalContext is canceled on Ctrl-C / SIGTERM so in-flight git processes
// are killed instead of orphaned.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// failIfAny turns per-repo failures into a non-zero exit status.
func failIfAny(results []runner.Result) error {
	if _, fail := runner.Summarize(results); fail > 0 {
		return cli.Exit(fmt.Sprintf("%d of %d repos failed", fail, len(results)), 1)
	}
	return nil
}

func runTUI(c *cli.Context) error {
	baseDir, err := resolveBaseDir(c.String("dir"))
	if err != nil {
		return err
	}
	if !report.StdoutIsTTY {
		return errors.New("the interactive TUI needs a terminal; use a subcommand (see gitops --help)")
	}
	return tui.Run(baseDir, c.Int("jobs"), splitList(c.String("repos")))
}

// resolveBaseDir turns the --dir flag into an absolute directory path,
// defaulting to the working directory.
func resolveBaseDir(dir string) (string, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine current directory: %w", err)
		}
		dir = cwd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

// splitList parses a comma-separated flag value.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// resolveRepos returns the repositories to operate on: the named ones, or
// every repository discovered in baseDir.
func resolveRepos(baseDir, repoFlag string) ([]string, error) {
	names := splitList(repoFlag)
	if len(names) == 0 {
		repos, err := git.Discover(baseDir)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", baseDir, err)
		}
		if len(repos) == 0 {
			return nil, fmt.Errorf("no git repositories found in %s (run `gitops init <org>` to clone one)", baseDir)
		}
		return repos, nil
	}
	repos := make([]string, 0, len(names))
	for _, name := range names {
		repoPath := filepath.Join(baseDir, name)
		if !git.IsRepo(repoPath) {
			return nil, fmt.Errorf("%s is not a git repository", repoPath)
		}
		repos = append(repos, repoPath)
	}
	return repos, nil
}
