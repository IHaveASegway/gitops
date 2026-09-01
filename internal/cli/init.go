package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"

	"github.com/IHaveASegway/gitops/internal/clone"
	"github.com/IHaveASegway/gitops/internal/format"
	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/report"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// Exit codes specific to init.
const (
	exitFailed  = 1 // at least one repo failed
	exitRefused = 2 // refused to clone (confirmation needed or duplicate detected)
)

func initCommand() *cli.Command {
	return &cli.Command{
		Name:      "init",
		Usage:     "Clone every repository of a GitHub organization (or user) into a subdirectory",
		ArgsUsage: "<org-url-or-name>",
		Description: `Lists every repository of the organization that your token can see and
clones the ones you don't have yet into <dir>/<org>/. Existing checkouts of
the same organization are detected first (by their origin remotes), so an
org is never duplicated by accident.

Accepted forms: https://github.com/acme, github.com/acme, acme,
git@github.com:acme/repo.git, https://ghe.example.com/acme

Authentication: GH_TOKEN, GITHUB_TOKEN, or the gh CLI's stored login.
Tokens are scoped to their host: GH_TOKEN/GITHUB_TOKEN are sent to
github.com only; GitHub Enterprise hosts use GH_ENTERPRISE_TOKEN,
GITHUB_ENTERPRISE_TOKEN, or gh auth login --hostname <host>.`,
		Flags: []cli.Flag{
			dirFlag(),
			&cli.StringFlag{Name: "repos", Aliases: []string{"r"}, Usage: "Comma-separated list of repo names to clone (default: all)"},
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Value: runner.DefaultJobs, Usage: "Number of parallel clones"},
			&cli.BoolFlag{Name: "here", Usage: "Clone directly into --dir instead of creating an <org> subdirectory"},
			&cli.StringFlag{Name: "protocol", Usage: "Clone protocol: ssh or https (default: gh's git_protocol setting, else https)"},
			&cli.BoolFlag{Name: "archived", Usage: "Include archived repositories"},
			&cli.BoolFlag{Name: "no-forks", Usage: "Skip forked repositories"},
			&cli.BoolFlag{Name: "dry-run", Usage: "Show what would be cloned and exit"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Do not ask for confirmation"},
			&cli.BoolFlag{Name: "force", Usage: "Clone even if an existing checkout of the org was detected"},
		},
		Action: runInit,
	}
}

func runInit(c *cli.Context) error {
	if c.NArg() != 1 {
		return errors.New("usage: gitops init <org-url-or-name> (see gitops init --help)")
	}
	ref, err := github.ParseOwner(c.Args().First())
	if err != nil {
		return err
	}
	protocol := strings.ToLower(c.String("protocol"))
	switch protocol {
	case "":
		protocol = github.DefaultProtocol(ref.Host)
	case "ssh", "https":
	default:
		return fmt.Errorf("--protocol must be ssh or https, got %q", protocol)
	}
	baseDir, err := resolveBaseDir(c.String("dir"))
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

	token, source := github.FindToken(ref.Host)
	auth := "unauthenticated — only public repositories will be listed"
	if token != "" {
		auth = "token from " + source
	}
	fmt.Printf("  Looking up %s on %s (%s)…\n", ref.Owner, ref.Host, auth)
	client := github.NewClient(ref.Host, token)
	if client.APIBase != github.DefaultAPIBase(ref.Host) {
		// An overridden API base receives the token; make that visible.
		fmt.Printf("  Using API base %s (GITOPS_GITHUB_API)\n", client.APIBase)
	}
	owner, err := client.LookupOwner(ctx, ref.Owner)
	if err != nil {
		return err
	}
	var progress func(page, total int)
	if report.StdoutIsTTY {
		progress = func(_, total int) { fmt.Printf("\r  Listing repositories… %d", total) }
	}
	repos, err := client.ListRepos(ctx, owner, progress)
	if err != nil {
		fmt.Println()
		return err
	}
	fmt.Printf("\r  Listed %s.%s\n", format.Plural(len(repos), "repository"), strings.Repeat(" ", 12))
	if len(repos) == 0 {
		return fmt.Errorf("%s has no repositories visible to you", owner.Login)
	}

	var only map[string]bool
	if names := splitList(c.String("repos")); len(names) > 0 {
		only = map[string]bool{}
		for _, n := range names {
			only[strings.ToLower(n)] = true
		}
	}
	plan := clone.BuildPlan(clone.Request{
		BaseDir: baseDir,
		Here:    c.Bool("here"),
		Host:    ref.Host,
		Owner:   owner,
		Repos:   repos,
		Filter:  clone.Filter{Only: only, IncludeArchived: c.Bool("archived"), SkipForks: c.Bool("no-forks")},
	})
	plan.Print(os.Stdout, protocol, c.Bool("dry-run") || len(plan.Considered()) <= 40)
	if c.Bool("dry-run") {
		return nil
	}
	toClone := plan.ToClone()
	if len(toClone) == 0 {
		fmt.Println("  Nothing to clone.")
		return nil
	}

	interactive := isatty.IsTerminal(os.Stdin.Fd())
	target := format.ShortenPath(plan.TargetDir)
	switch {
	case plan.HasWarnings() && !c.Bool("force"):
		if !interactive || c.Bool("yes") {
			return cli.Exit("refusing to clone: an existing checkout of this organization was found (see above). Re-run with --here/-d as suggested, or pass --force to clone anyway.", exitRefused)
		}
		if !confirm(fmt.Sprintf("Clone %s into %s anyway?", format.Plural(len(toClone), "repo"), target), false) {
			fmt.Println("  Aborted.")
			return nil
		}
	case !c.Bool("yes"):
		if !interactive {
			return cli.Exit("refusing to clone without confirmation; pass --yes", exitRefused)
		}
		if !confirm(fmt.Sprintf("Clone %s into %s?", format.Plural(len(toClone), "repo"), target), true) {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	names := make([]string, len(toClone))
	for i, e := range toClone {
		names[i] = e.Repo.Name
	}
	nameW := report.NameColumnWidth(names, 12, 48)
	total := len(toClone)
	done := 0
	fmt.Println()
	results := clone.Run(ctx, toClone, clone.Options{Host: ref.Host, Protocol: protocol, Token: token}, c.Int("jobs"),
		func(ev runner.Event) {
			if ev.Started {
				return
			}
			done++
			mark, detail := report.Paint("32", "✓"), ev.Result.FirstLine()
			if !ev.Result.Success {
				mark, detail = report.Paint("31", "✗"), report.Paint("31", detail)
			}
			fmt.Printf("  [%*d/%d] %s %-*s  %s\n", len(fmt.Sprint(total)), done, total, mark, nameW, ev.Result.Repo, detail)
		})

	ok, fail := runner.Summarize(results)
	fmt.Printf("\n  Cloned %d of %s into %s", ok, format.Plural(total, "repo"), target)
	if fail > 0 {
		fmt.Printf(" — %s\n", report.Paint("31", fmt.Sprintf("%d failed", fail)))
		for _, r := range results {
			if !r.Success {
				fmt.Printf("    %s %s: %s\n", report.Paint("31", "✗"), r.Repo, r.Error)
			}
		}
	}
	fmt.Println()
	if ctx.Err() != nil {
		return cli.Exit("interrupted", 130)
	}
	return failIfAny(results)
}

// confirm asks a yes/no question on the terminal.
func confirm(question string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	fmt.Printf("  %s %s ", question, hint)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return defaultYes
	case "y", "yes":
		return true
	}
	return false
}
