package report

import (
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/runner"
)

func TestPrintResults(t *testing.T) {
	Color = false
	var b strings.Builder
	PrintResults(&b, "Status results", []runner.Result{
		{Repo: "alpha", Success: true, Output: "[main] clean"},
		{Repo: "beta", Success: true, Output: "[main] 1 changed\n M README.md"},
		{Repo: "gamma", Error: "checkout main: nope"},
	})
	out := b.String()
	for _, want := range []string{"Status results", "✓ alpha", "[main] clean", " M README.md", "✗ gamma", "checkout main: nope", "Total: 3  ✓ 2  ✗ 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\033[") {
		t.Error("colors should be off")
	}
	Color = true
	if got := Paint("32", "x"); got != "\033[32mx\033[0m" {
		t.Errorf("Paint = %q", got)
	}
	Color = false
}

func TestNameColumnWidth(t *testing.T) {
	if w := NameColumnWidth([]string{"a", "longer-name"}, 4, 8); w != 8 {
		t.Errorf("got %d", w)
	}
	if w := NameColumnWidth(nil, 4, 8); w != 4 {
		t.Errorf("got %d", w)
	}
}
