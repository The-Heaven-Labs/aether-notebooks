package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func CellsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cells",
		Short: "Manage cells within a notebook",
	}

	cmd.AddCommand(
		func() *cobra.Command {
			var nbID, source, lang string
			c := &cobra.Command{
				Use:   "execute",
				Short: "Execute a cell",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					// Create cell then execute
					var cell struct {
						ID string `json:"id"`
					}
					if err := cl.PostJSON(
						"/api/v1/notebooks/"+nbID+"/cells",
						map[string]string{"type": "code", "language": lang, "source": source},
						&cell,
					); err != nil {
						return err
					}
					var result interface{}
					if err := cl.PostJSON(
						fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/execute", nbID, cell.ID),
						map[string]string{},
						&result,
					); err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVar(&nbID, "notebook", "", "Notebook ID (required)")
			c.Flags().StringVar(&source, "source", "", "SQL source (required)")
			c.Flags().StringVar(&lang, "lang", "sql", "Language")
			c.MarkFlagRequired("notebook")
			c.MarkFlagRequired("source")
			return c
		}(),
	)
	return cmd
}
