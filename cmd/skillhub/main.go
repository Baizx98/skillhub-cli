package main

import (
	"fmt"
	"os"

	"github.com/baizx98/skillhub-cli/internal/skillhub"
)

var version = "dev"

func main() {
	app, err := skillhub.New(os.Stdout, os.Stderr, os.Stdin)
	if err == nil {
		err = app.Run(os.Args[1:], version)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "skillhub:", err)
		os.Exit(1)
	}
}
