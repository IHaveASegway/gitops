package github

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// FindToken locates a token for host: environment variables first (matching
// the gh CLI's own precedence), then gh's stored credentials. The returned
// source is a short label for display, e.g. "$GITHUB_TOKEN" or "gh auth".
func FindToken(host string) (token, source string) {
	envs := []string{"GH_TOKEN", "GITHUB_TOKEN"}
	if !strings.EqualFold(host, "github.com") {
		envs = append([]string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}, envs...)
	}
	for _, name := range envs {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, "$" + name
		}
	}
	if out, ok := gh("auth", "token", "--hostname", host); ok && out != "" {
		return out, "gh auth"
	}
	return "", ""
}

// DefaultProtocol returns "ssh" when the gh CLI is configured for SSH on
// host, otherwise "https".
func DefaultProtocol(host string) string {
	if out, ok := gh("config", "get", "--host", host, "git_protocol"); ok && out == "ssh" {
		return "ssh"
	}
	return "https"
}

// gh runs the gh CLI if it is installed and returns its trimmed output.
func gh(args ...string) (string, bool) {
	bin, err := exec.LookPath("gh")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
