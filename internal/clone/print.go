package clone

import (
	"fmt"
	"io"
	"strings"

	"github.com/IHaveASegway/gitops/internal/format"
	"github.com/IHaveASegway/gitops/internal/report"
)

// Print writes the plan to w. With listAll every considered repository is
// listed; otherwise only conflicts are itemized.
func (p *Plan) Print(w io.Writer, protocol string, listAll bool) {
	kind := "Organization"
	if !p.Owner.IsOrg() {
		kind = "User"
	}
	fmt.Fprintf(w, "\n  %-13s %s  %s\n", report.Paint("1", kind+":"), p.Owner.Login, report.Paint("2", p.OwnerURL()))
	fmt.Fprintf(w, "  %-13s %s\n", report.Paint("1", "Target:"), format.ShortenPath(p.TargetDir))
	fmt.Fprintf(w, "  %-13s %s\n", report.Paint("1", "Protocol:"), protocol)
	fmt.Fprintf(w, "  %-13s %s\n", report.Paint("1", "Repos:"), p.SummaryLine())
	if len(p.Missing) > 0 {
		fmt.Fprintf(w, "  %-13s %s\n", report.Paint("33", "Not in org:"), strings.Join(p.Missing, ", "))
	}
	if p.Foreign > 0 {
		fmt.Fprintf(w, "  %-13s %s already in the target dir belong to other owners and are left untouched\n",
			report.Paint("2", "Note:"), format.Plural(p.Foreign, "repo"))
	}

	entries := p.Considered()
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Repo.Name
	}
	nameW := report.NameColumnWidth(names, 12, 48)
	printed := false
	for _, e := range entries {
		if !listAll && e.Action != ActionConflict {
			continue
		}
		if !printed {
			fmt.Fprintln(w)
			printed = true
		}
		var mark, detail string
		switch e.Action {
		case ActionClone:
			mark, detail = report.Paint("32", "+"), "clone"
			if e.Repo.Private {
				detail += " (private)"
			}
		case ActionExists:
			mark, detail = report.Paint("2", "="), "already present"
			if e.Reason != "" {
				detail += " " + e.Reason
			}
		case ActionArchived:
			mark, detail = report.Paint("2", "-"), "archived, skipped"
		case ActionFork:
			mark, detail = report.Paint("2", "-"), "fork, skipped"
		case ActionConflict:
			mark, detail = report.Paint("31", "!"), report.Paint("31", e.Reason)
		}
		fmt.Fprintf(w, "  %s %-*s  %s\n", mark, nameW, e.Repo.Name, report.Paint("2", detail))
	}

	for _, warn := range p.Warnings {
		fmt.Fprintln(w)
		for i, line := range p.WarningLines(warn) {
			if i == 0 {
				fmt.Fprintf(w, "  %s %s\n", report.Paint("33", "⚠"), report.Paint("33", line))
			} else {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}
	fmt.Fprintln(w)
}
