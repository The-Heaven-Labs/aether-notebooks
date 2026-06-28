package main

import (
	"fmt"
	"os"

	"github.com/the-heaven-labs/aether/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "aether",
		Short: "Aether Notebooks CLI",
	}

	root.AddCommand(
		cli.LoginCmd(),
		cli.LogoutCmd(),
		cli.NotebooksCmd(),
		cli.CellsCmd(),
		cli.ConnectorsCmd(),
		cli.SeedCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
