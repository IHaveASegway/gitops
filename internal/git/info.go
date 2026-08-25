package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Info is a quick snapshot of a repository's state.
type Info struct {
	Branch   string // branch name, or the short commit hash when detached
	Detached bool
	Dirty    int // number of changed paths (git status --porcelain lines)
	Ahead    int // commits ahead of the upstream branch
	Behind   int // commits behind the upstream branch
	Err      string
}

// Tag renders a compact label such as "main ↑1 ↓2".
func (i Info) Tag() string {
	s := i.Branch
	if i.Detached {
		s = "detached@" + i.Branch
	}
	if i.Ahead > 0 {
		s += fmt.Sprintf(" ↑%d", i.Ahead)
	}
	if i.Behind > 0 {
		s += fmt.Sprintf(" ↓%d", i.Behind)
	}
	return s
}

// Inspect gathers branch, dirtiness and ahead/behind counts for repo.
func Inspect(ctx context.Context, repo string) Info {
	var info Info
	if out, err := Run(ctx, repo, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && out != "" {
		info.Branch = out
	} else {
		info.Detached = true
		info.Branch = "?"
		if out, err := Run(ctx, repo, "rev-parse", "--short", "HEAD"); err == nil && out != "" {
			info.Branch = out
		}
	}
	status, err := Run(ctx, repo, "status", "--porcelain")
	if err != nil {
		info.Err = err.Error()
		return info
	}
	if status != "" {
		info.Dirty = len(strings.Split(status, "\n"))
	}
	if out, err := Run(ctx, repo, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
		if f := strings.Fields(out); len(f) == 2 {
			info.Ahead, _ = strconv.Atoi(f[0])
			info.Behind, _ = strconv.Atoi(f[1])
		}
	}
	return info
}
