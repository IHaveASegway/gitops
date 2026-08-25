package git

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IsRepo reports whether dir is a git working tree (a ".git" directory, or
// the ".git" file used by worktrees and submodules).
func IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// gitDir resolves the actual .git directory of a working tree, following
// "gitdir:" pointer files.
func gitDir(repo string) string {
	p := filepath.Join(repo, ".git")
	info, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return p
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(repo, target)
	}
	return target
}

// OriginURL reads the "origin" remote URL straight from the git config,
// which is far cheaper than spawning git when scanning many directories.
// The stored URL is returned as-is, before any insteadOf rewriting.
func OriginURL(repo string) (string, bool) {
	dir := gitDir(repo)
	if dir == "" {
		return "", false
	}
	cfgPath := filepath.Join(dir, "config")
	if data, err := os.ReadFile(filepath.Join(dir, "commondir")); err == nil {
		// Worktrees keep their config in the shared common directory.
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		cfgPath = filepath.Join(common, "config")
	}
	f, err := os.Open(cfgPath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			inOrigin = strings.EqualFold(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "url" {
			continue
		}
		if val = strings.Trim(strings.TrimSpace(val), `"`); val != "" {
			return val, true
		}
	}
	return "", false
}

// LocalRepo is a repository found on disk.
type LocalRepo struct {
	Path      string    // absolute path of the working tree
	Remote    RemoteRef // parsed "origin" remote, when present and recognizable
	HasRemote bool
}

// Scan finds repositories up to maxDepth directory levels below root
// (1 = direct children only). It never descends into a repository, and
// visits hidden directories only when includeHidden is set.
func Scan(root string, maxDepth int, includeHidden bool) []LocalRepo {
	var out []LocalRepo
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == ".git" || (!includeHidden && strings.HasPrefix(e.Name(), ".")) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if IsRepo(p) {
				lr := LocalRepo{Path: p}
				if u, ok := OriginURL(p); ok {
					if ref, ok := ParseRemoteURL(u); ok {
						lr.Remote, lr.HasRemote = ref, true
					}
				}
				out = append(out, lr)
				continue
			}
			if depth < maxDepth {
				walk(p, depth+1)
			}
		}
	}
	walk(root, 1)
	return out
}

// Discover returns the direct subdirectories of baseDir that are
// repositories, in directory order.
func Discover(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".git" {
			continue
		}
		if p := filepath.Join(baseDir, e.Name()); IsRepo(p) {
			repos = append(repos, p)
		}
	}
	return repos, nil
}
