package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bagakit/issues/internal/app"
)

var version = "dev"

func main() {
	application, err := app.New(version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "issues: %v\n", err)
		os.Exit(1)
	}

	os.Exit(application.Run(context.Background(), os.Args[1:]))
}
