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

	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"

	"github.com/IHaveASegway/gitops/internal/buildinfo"
	"github.com/IHaveASegway/gitops/internal/format"
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
	branchNameOK := func(c *cli.Context) error { return git.CheckBranchName(c.String("name")) }
	return &cli.App{
		Name:    "gitops",
		Usage:   "Mass git operations across multiple repositories",
		Version: buildinfo.Version(),
		Flags:   append(sharedFlags(), dirFlag()),
		Action:  runTUI,
		Commands: []*cli.Command{
			repoCommand(opCommand{
				name: "pull", usage: "Checkout default branch and pull latest",
				flags: []cli.Flag{skipSubmodulesFlag()},
				build: func(c *cli.Context) runner.Func { return ops.Pull(c.Bool("skip-submodules")) },
			}),
			repoCommand(opCommand{
				name: "sync", usage: "Stash changes, checkout default branch, pull, pop stash",
				flags: []cli.Flag{skipSubmodulesFlag()},
				build: func(c *cli.Context) runner.Func { return ops.Sync(c.Bool("skip-submodules")) },
			}),
			repoCommand(opCommand{
				name: "status", usage: "Show branch, ahead/behind and working tree status for all repos",
				build: func(*cli.Context) runner.Func { return ops.Status },
			}),
			repoCommand(opCommand{
				name: "branch", usage: "Create a new branch from the default branch",
				flags: []cli.Flag{&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "Name of the new branch", Required: true}, skipSubmodulesFlag()},
				check: branchNameOK,
				build: func(c *cli.Context) runner.Func { return ops.CreateBranch(c.String("name"), c.Bool("skip-submodules")) },
			}),
			repoCommand(opCommand{
				name: "checkout", usage: "Checkout an existing branch",
				flags: []cli.Flag{&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "Branch name to checkout", Required: true}, skipSubmodulesFlag()},
				check: func(c *cli.Context) error { return git.CheckRefArg(c.String("name")) },
				build: func(c *cli.Context) runner.Func { return ops.Checkout(c.String("name"), c.Bool("skip-submodules")) },
			}),
			repoCommand(opCommand{
				name: "push", usage: "Stage all changes, commit, and push the current branch",
				flags: []cli.Flag{
					&cli.StringFlag{Name: "message", Aliases: []string{"m"}, Usage: "Commit message", Required: true},
					yesFlag(),
				},
				build: func(c *cli.Context) runner.Func { return ops.Push(c.String("message")) },
				warn: func(c *cli.Context, n int) string {
					return fmt.Sprintf("push will stage all changes (except %s), commit %q and push the current branch in %s.",
						ops.ExcludedJunk(), strings.TrimSpace(c.String("message")), format.Plural(n, "repository"))
				},
			}),
			repoCommand(opCommand{
				name: "reset", usage: "Discard ALL local changes, force checkout default branch, pull",
				flags: []cli.Flag{yesFlag(), skipSubmodulesFlag()},
				build: func(c *cli.Context) runner.Func { return ops.Reset(c.Bool("skip-submodules")) },
				warn: func(_ *cli.Context, n int) string {
					return fmt.Sprintf("reset will permanently discard all uncommitted changes and untracked files in %s, then force-checkout the default branch and pull.",
						format.Plural(n, "repository"))
				},
			}),
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

func yesFlag() cli.Flag {
	return &cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Do not ask for confirmation"}
}

func skipSubmodulesFlag() cli.Flag {
	return &cli.BoolFlag{Name: "skip-submodules", Usage: "Do not update submodules (git submodule update --init --recursive)"}
}

// opCommand describes one mass-git subcommand.
type opCommand struct {
	name, usage string
	flags       []cli.Flag                         // command-specific flags
	build       func(*cli.Context) runner.Func     // the operation to run per repo
	check       func(*cli.Context) error           // optional flag validation before anything runs
	warn        func(c *cli.Context, n int) string // non-nil marks the command destructive: it must be confirmed
}

// repoCommand builds a subcommand that runs one git operation across repos.
func repoCommand(op opCommand) *cli.Command {
	return &cli.Command{
		Name:  op.name,
		Usage: op.usage,
		Flags: append(append(sharedFlags(), dirFlag()), op.flags...),
		Action: func(c *cli.Context) error {
			if op.check != nil {
				if err := op.check(c); err != nil {
					return err
				}
			}
			baseDir, err := resolveBaseDir(c.String("dir"))
			if err != nil {
				return err
			}
			repos, err := resolveRepos(baseDir, c.String("repos"))
			if err != nil {
				return err
			}
			if op.warn != nil {
				names := make([]string, len(repos))
				for i, r := range repos {
					names[i] = filepath.Base(r)
				}
				ok, err := confirmDestructive(op.name, op.warn(c, len(repos)), names, c.Bool("yes"))
				if err != nil || !ok {
					return err
				}
			}
			ctx, stop := signalContext()
			defer stop()
			results := runner.Run(ctx, repos, op.build(c), c.Int("jobs"), nil)
			report.PrintResults(os.Stdout, strings.ToUpper(op.name[:1])+op.name[1:]+" results", results)
			return failIfAny(results)
		},
	}
}

// confirmDestructive gates a destructive command like the TUI does: with
// --yes it proceeds, in a terminal it asks (defaulting to No), and without
// either it refuses with exit code 2 so scripts must opt in explicitly.
func confirmDestructive(name, warning string, repoNames []string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false, cli.Exit(fmt.Sprintf("refusing to %s without confirmation; re-run with --yes", name), exitRefused)
	}
	fmt.Println("  " + report.Paint("33", "⚠ ") + warning)
	fmt.Println("    " + format.JoinNames(repoNames, 15))
	if !confirm("Continue?", false) {
		fmt.Println("  Aborted.")
		return false, nil
	}
	return true, nil
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
		// Repo names address direct children of baseDir; anything with an
		// interior path separator or a dot-dot could reach (and reset or
		// push) an unrelated repository elsewhere on disk. A single trailing
		// slash is tolerated — shell tab-completion adds one to "myrepo/".
		clean := strings.TrimRight(name, `/\`)
		if clean == "" || clean == "." || clean == ".." || strings.ContainsAny(clean, `/\`) {
			return nil, fmt.Errorf("invalid repo name %q: use bare directory names inside %s", name, baseDir)
		}
		repoPath := filepath.Join(baseDir, clean)
		if !git.IsRepo(repoPath) {
			return nil, fmt.Errorf("%s is not a git repository", repoPath)
		}
		repos = append(repos, repoPath)
	}
	return repos, nil
}
