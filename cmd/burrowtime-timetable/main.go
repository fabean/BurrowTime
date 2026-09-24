package main

import (
	"fmt"
	"os"

	"github.com/fabean/BurrowTime/internal/timetable"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Timetable connector protocol 1. Use burrowtime timetable --help for setup and sync.")
		return
	}
	if err := timetable.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
