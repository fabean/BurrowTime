package main

import (
	"fmt"
	"os"

	"github.com/josh/burrowtime/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, cli.FormatError(err))
		os.Exit(cli.ExitCode(err))
	}
}
