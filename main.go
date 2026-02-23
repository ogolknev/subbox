package main

import (
	"fmt"
	"os"

	"subbox/app"
)

func main() {
	if err := app.RunCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
