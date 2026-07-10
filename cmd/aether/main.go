package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/the-heaven-labs/aether/internal/cli"
)

func main() {
	root := &cobra.Command{
		Use:   "aether",
		Short: "Aether Notebooks CLI",
	}

	root.AddCommand(
		cli.ConfigCmd(),
		cli.LoginCmd(),
		cli.LogoutCmd(),
		cli.RegisterCmd(),
		cli.NotebooksCmd(),
		cli.CellsCmd(),
		cli.ConnectorsCmd(),
		cli.DashboardsCmd(),
		cli.FoldersCmd(),
		cli.GroupsCmd(),
		cli.ACLCmd(),
		cli.MembersCmd(),
		cli.OrgCmd(),
		cli.TokensCmd(),
		cli.TemplatesCmd(),
		cli.TrashCmd(),
		cli.HomeCmd(),
		cli.RecentCmd(),
		cli.AttachmentsCmd(),
		cli.AuditCmd(),
		cli.MOTDCmd(),
		cli.AgentsCmd(),
		cli.ModelConfigsCmd(),
		cli.SkillsCmd(),
		cli.ToolsCmd(),
		cli.MCPServersCmd(),
		cli.SSOCmd(),
		cli.AdminCmd(),
		cli.SeedCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
