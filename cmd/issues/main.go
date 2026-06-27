package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bagakit/issues/internal/cli"
)

var version = "dev"

func main() {
	app, err := cli.New(version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "issues: %v\n", err)
		os.Exit(1)
	}

	os.Exit(app.Run(context.Background(), os.Args[1:]))
}
