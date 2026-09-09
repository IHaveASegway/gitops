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

// PrintOrphans reports checkouts the organization listing did not mention.
// Nothing here is removed: pruning is a separate, explicitly requested step,
// so this only ever tells the user what it found and how to act on it.
func PrintOrphans(w io.Writer, orphans []Orphan, pruning bool) {
	if len(orphans) == 0 {
		return
	}
	var gone, renamed, unverified, present []Orphan
	for _, o := range orphans {
		switch o.Status {
		case OrphanGone:
			gone = append(gone, o)
		case OrphanRenamed:
			renamed = append(renamed, o)
		case OrphanPresent:
			present = append(present, o)
		default:
			unverified = append(unverified, o)
		}
	}

	names := make([]string, len(orphans))
	for i, o := range orphans {
		names[i] = o.Name
	}
	nameW := report.NameColumnWidth(names, 12, 48)

	if len(gone) > 0 {
		fmt.Fprintf(w, "  %s %s\n", report.Paint("33", "⚠"),
			report.Paint("33", fmt.Sprintf("%s here no longer on GitHub:", format.Plural(len(gone), "repo"))))
		for _, o := range gone {
			detail := "gone (404)"
			if len(o.Blockers) > 0 {
				detail += " · " + report.Paint("31", strings.Join(o.Blockers, ", "))
			} else {
				detail += " · clean, nothing unpushed"
			}
			fmt.Fprintf(w, "  %s %-*s  %s\n", report.Paint("33", "~"), nameW, o.Name, report.Paint("2", detail))
		}
		if !pruning {
			fmt.Fprintf(w, "\n    %s\n", "These were NOT removed. To remove the clean ones:")
			fmt.Fprintf(w, "    %s\n", report.Paint("2", "gitops init <org> --prune"))
		}
	}

	// A repository that still resolves was never deleted; saying so matters
	// more than the ones that were, because it is the case where a naive
	// "not in the listing" check would have deleted live work.
	if len(renamed) > 0 {
		fmt.Fprintf(w, "\n  %s %s\n", report.Paint("2", "i"),
			report.Paint("2", fmt.Sprintf("%s renamed or transferred on GitHub (kept):", format.Plural(len(renamed), "repo"))))
		for _, o := range renamed {
			fmt.Fprintf(w, "  %s %-*s  %s\n", report.Paint("2", "→"), nameW, o.Name, report.Paint("2", "now "+o.NewName))
		}
	}
	if len(present) > 0 {
		fmt.Fprintf(w, "\n  %s %s\n", report.Paint("2", "i"),
			report.Paint("2", fmt.Sprintf("%s still on GitHub but absent from the listing (kept):", format.Plural(len(present), "repo"))))
		for _, o := range present {
			fmt.Fprintf(w, "  %s %-*s  %s\n", report.Paint("2", "="), nameW, o.Name, report.Paint("2", "visibility or filter"))
		}
	}
	if len(unverified) > 0 {
		fmt.Fprintf(w, "\n  %s %s\n", report.Paint("33", "?"),
			report.Paint("33", fmt.Sprintf("%s could not be checked against GitHub (kept):", format.Plural(len(unverified), "repo"))))
		for _, o := range unverified {
			fmt.Fprintf(w, "  %s %-*s  %s\n", report.Paint("33", "?"), nameW, o.Name, report.Paint("2", o.Detail))
		}
	}
	fmt.Fprintln(w)
}
