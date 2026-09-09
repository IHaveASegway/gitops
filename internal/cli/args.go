package cli

import (
	"strings"

	"github.com/urfave/cli/v2"
)

// interspersedArgs lets flags follow positional arguments
// ("gitops init acme --dry-run"): urfave/cli v2 stops parsing flags at the
// first positional, so the subcommand's flags are moved in front of it.
func interspersedArgs(app *cli.App, args []string) []string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if a != "--" && takesValue(app.Flags, a) && !strings.Contains(a, "=") {
				i++ // skip the flag's value
			}
			continue
		}
		cmd := app.Command(a)
		if cmd == nil {
			return args
		}
		var flags, positional []string
		rest := args[i+1:]
		for j := 0; j < len(rest); j++ {
			r := rest[j]
			if r == "--" {
				positional = append(positional, rest[j+1:]...)
				break
			}
			if strings.HasPrefix(r, "-") && len(r) > 1 {
				// A value-taking flag with nothing after it would otherwise be
				// moved in front of the positionals, letting urfave bind the
				// org name as its value and report a misleading usage error.
				// Hand the original argv back so urfave says what is wrong.
				if takesValue(cmd.Flags, r) && !strings.Contains(r, "=") && j+1 >= len(rest) {
					return args
				}
				flags = append(flags, r)
				if takesValue(cmd.Flags, r) && !strings.Contains(r, "=") {
					j++
					flags = append(flags, rest[j])
				}
				continue
			}
			positional = append(positional, r)
		}
		out := append([]string{}, args[:i+1]...)
		out = append(out, flags...)
		// Re-emit the separator so a positional is never re-parsed as a flag.
		// Reordering drops the original "--", which is the one thing it exists
		// to do; without this a "--"-protected operand is parsed as a flag.
		if len(positional) > 0 {
			out = append(out, "--")
		}
		return append(out, positional...)
	}
	return args
}

// takesValue reports whether arg names a non-boolean flag in flags.
func takesValue(flags []cli.Flag, arg string) bool {
	name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	for _, f := range flags {
		for _, n := range f.Names() {
			if n == name {
				_, isBool := f.(*cli.BoolFlag)
				return !isBool
			}
		}
	}
	return false
}
