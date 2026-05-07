package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:    "gitops",
		Usage:   "Mass git operations across multiple repositories",
		Version: "1.0.0",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repos",
				Aliases: []string{"r"},
				Usage:   "Comma-separated list of repos to operate on",
			},
			&cli.StringFlag{
				Name:    "dir",
				Aliases: []string{"d"},
				Usage:   "Base directory containing repos (default: current directory)",
			},
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"R"},
				Usage:   "Recurse one level into subdirectories to find repos",
			},
		},
		Action: func(c *cli.Context) error {
			baseDir := resolveBaseDir(c.String("dir"))
			var repos []string
			var err error
			if c.Bool("recursive") {
				repos, err = discoverReposRecursive(baseDir)
			} else {
				repos, err = discoverRepos(baseDir)
			}
			if err != nil {
				return fmt.Errorf("failed to discover repos: %w", err)
			}
			if len(repos) == 0 {
				return fmt.Errorf("no git repositories found in %s", baseDir)
			}
			p := tea.NewProgram(newTUIModel(baseDir, repos), tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
		Commands: []*cli.Command{
			{
				Name:  "pull",
				Usage: "Checkout default branch and pull latest",
				Flags: sharedFlags(),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opPull)
					annotateGroups(baseDir, repos, results)
					printResults("Pull", results, skipped)
					return nil
				},
			},
			{
				Name:  "sync",
				Usage: "Stash changes, checkout default branch, pull, pop stash",
				Flags: sharedFlags(),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opSync)
					annotateGroups(baseDir, repos, results)
					printResults("Sync", results, skipped)
					return nil
				},
			},
			{
				Name:  "reset",
				Usage: "Discard all changes, force checkout default branch, pull",
				Flags: sharedFlags(),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opReset)
					annotateGroups(baseDir, repos, results)
					printResults("Reset", results, skipped)
					return nil
				},
			},
			{
				Name:  "branch",
				Usage: "Create a new branch from the default branch",
				Flags: append(sharedFlags(), &cli.StringFlag{
					Name:     "name",
					Aliases:  []string{"n"},
					Usage:    "Name of the new branch",
					Required: true,
				}),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opCreateBranch(c.String("name")))
					annotateGroups(baseDir, repos, results)
					printResults("Branch", results, skipped)
					return nil
				},
			},
			{
				Name:  "push",
				Usage: "Stage all changes, commit, and push",
				Flags: append(sharedFlags(), &cli.StringFlag{
					Name:     "message",
					Aliases:  []string{"m"},
					Usage:    "Commit message",
					Required: true,
				}),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opPush(c.String("message")))
					annotateGroups(baseDir, repos, results)
					printResults("Push", results, skipped)
					return nil
				},
			},
			{
				Name:  "checkout",
				Usage: "Checkout an existing branch",
				Flags: append(sharedFlags(), &cli.StringFlag{
					Name:     "name",
					Aliases:  []string{"n"},
					Usage:    "Branch name to checkout",
					Required: true,
				}),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opCheckout(c.String("name")))
					annotateGroups(baseDir, repos, results)
					printResults("Checkout", results, skipped)
					return nil
				},
			},
			{
				Name:  "status",
				Usage: "Show git status for all repos",
				Flags: sharedFlags(),
				Action: func(c *cli.Context) error {
					baseDir := resolveBaseDir(c.String("dir"))
					repos, skipped, err := resolveRepos(baseDir, c.String("repos"), c.String("exclude"), c.Bool("recursive"))
					if err != nil {
						return err
					}
					results := runParallel(repos, opStatus)
					annotateGroups(baseDir, repos, results)
					printResults("Status", results, skipped)
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func sharedFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "exclude",
			Aliases: []string{"x"},
			Usage:   "Comma-separated list of repos to exclude",
		},
		&cli.StringFlag{
			Name:    "repos",
			Aliases: []string{"r"},
			Usage:   "Comma-separated list of repos to operate on",
		},
		&cli.StringFlag{
			Name:    "dir",
			Aliases: []string{"d"},
			Usage:   "Base directory containing repos (default: cwd)",
		},
		&cli.BoolFlag{
			Name:    "recursive",
			Aliases: []string{"R"},
			Usage:   "Recurse one level into subdirectories to find repos",
		},
	}
}

func resolveBaseDir(dir string) string {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return dir
		}
		return abs
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine current directory: %v\n", err)
		os.Exit(1)
	}
	return cwd
}

func resolveRepos(baseDir, repoFlag, excludeFlag string, recursive bool) ([]string, int, error) {
	excludeSet := make(map[string]bool)
	if excludeFlag != "" {
		for _, name := range strings.Split(excludeFlag, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				excludeSet[name] = true
			}
		}
	}

	var repos []string
	if repoFlag != "" {
		names := strings.Split(repoFlag, ",")
		repos = make([]string, 0, len(names))
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			repoPath := filepath.Join(baseDir, name)
			if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
				return nil, 0, fmt.Errorf("%s is not a git repository", name)
			}
			repos = append(repos, repoPath)
		}
	} else {
		var err error
		if recursive {
			repos, err = discoverReposRecursive(baseDir)
		} else {
			repos, err = discoverRepos(baseDir)
		}
		if err != nil {
			return nil, 0, err
		}
	}

	skipped := 0
	if len(excludeSet) > 0 {
		before := len(repos)
		filtered := repos[:0]
		for _, r := range repos {
			if !excludeSet[filepath.Base(r)] {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
		skipped = before - len(repos)
	}

	return repos, skipped, nil
}
