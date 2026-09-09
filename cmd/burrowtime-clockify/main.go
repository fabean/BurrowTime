package main

import (
	"fmt"
	"github.com/fabean/BurrowTime/internal/clockify"
	"os"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Clockify connector protocol 1. Use burrowtime clockify --help for setup and sync.")
		return
	}
	if err := clockify.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
