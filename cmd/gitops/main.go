// Command gitops runs git operations across many repositories at once and
// clones whole GitHub organizations. Run `gitops --help` for usage.
package main

import (
	"fmt"
	"os"

	"github.com/IHaveASegway/gitops/internal/cli"
)

func main() {
	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
