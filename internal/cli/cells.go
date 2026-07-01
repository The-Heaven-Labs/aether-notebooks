package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListCells(notebookID string) ([]Cell, error) {
	var nb map[string]interface{}
	if err := c.GetJSON("/api/v1/notebooks/"+notebookID, &nb); err != nil {
		return nil, err
	}
	cellsRaw, ok := nb["cells"]
	if !ok {
		return nil, nil
	}
	cellsJSON, err := json.Marshal(cellsRaw)
	if err != nil {
		return nil, err
	}
	var cells []Cell
	if err := json.Unmarshal(cellsJSON, &cells); err != nil {
		return nil, err
	}
	return cells, nil
}

func (c *Client) GetCell(notebookID, cellID string) (*Cell, error) {
	cells, err := c.ListCells(notebookID)
	if err != nil {
		return nil, err
	}
	for _, cell := range cells {
		if cell.ID == cellID {
			return &cell, nil
		}
	}
	return nil, fmt.Errorf("cell %s not found in notebook %s", cellID, notebookID)
}

func (c *Client) UpdateCell(notebookID, cellID, source string) (*Cell, error) {
	body := map[string]string{"source": source}
	var cell Cell
	if err := c.PutJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID, body, &cell); err != nil {
		return nil, err
	}
	return &cell, nil
}

func (c *Client) DeleteCell(notebookID, cellID string) error {
	return c.DeleteJSON("/api/v1/notebooks/" + notebookID + "/cells/" + cellID)
}

func (c *Client) DuplicateCell(notebookID, cellID string) (*Cell, error) {
	var cell Cell
	if err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID+"/duplicate", nil, &cell); err != nil {
		return nil, err
	}
	return &cell, nil
}

func (c *Client) ListCellVersions(notebookID, cellID string) ([]CellVersion, error) {
	var versions []CellVersion
	if err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID+"/versions", &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func (c *Client) RestoreCellVersion(notebookID, cellID, versionID string) error {
	return c.PostJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID+"/versions/"+versionID+"/restore", nil, nil)
}

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
		&cobra.Command{
			Use:   "list <notebook-id>",
			Short: "List cells in a notebook",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				cells, err := c.ListCells(args[0])
				if err != nil {
					return err
				}
				PrintJSON(cells)
				return nil
			},
		},
		&cobra.Command{
			Use:   "get <notebook-id> <cell-id>",
			Short: "Get a cell",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				cell, err := c.GetCell(args[0], args[1])
				if err != nil {
					return err
				}
				PrintJSON(cell)
				return nil
			},
		},
		func() *cobra.Command {
			var source string
			c := &cobra.Command{
				Use:   "update <notebook-id> <cell-id>",
				Short: "Update a cell's source",
				Args:  cobra.ExactArgs(2),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					cell, err := cl.UpdateCell(args[0], args[1], source)
					if err != nil {
						return err
					}
					PrintJSON(cell)
					return nil
				},
			}
			c.Flags().StringVar(&source, "source", "", "New cell source (required)")
			c.MarkFlagRequired("source")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <notebook-id> <cell-id>",
			Short: "Delete a cell",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteCell(args[0], args[1]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "duplicate <notebook-id> <cell-id>",
			Short: "Duplicate a cell",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				cell, err := c.DuplicateCell(args[0], args[1])
				if err != nil {
					return err
				}
				PrintJSON(cell)
				return nil
			},
		},
		func() *cobra.Command {
			versionsCmd := &cobra.Command{
				Use:   "versions",
				Short: "Manage cell versions",
			}
			versionsCmd.AddCommand(
				&cobra.Command{
					Use:   "list <notebook-id> <cell-id>",
					Short: "List cell versions",
					Args:  cobra.ExactArgs(2),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						versions, err := c.ListCellVersions(args[0], args[1])
						if err != nil {
							return err
						}
						PrintJSON(versions)
						return nil
					},
				},
				&cobra.Command{
					Use:   "restore <notebook-id> <cell-id> <version-id>",
					Short: "Restore a cell to a previous version",
					Args:  cobra.ExactArgs(3),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.RestoreCellVersion(args[0], args[1], args[2]); err != nil {
							return err
						}
						fmt.Println("Version restored.")
						return nil
					},
				},
			)
			return versionsCmd
		}(),
	)
	return cmd
}
