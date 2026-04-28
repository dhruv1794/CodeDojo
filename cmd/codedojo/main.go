package main

import (
	"os"

	"github.com/dhruvmishra/codedojo/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
