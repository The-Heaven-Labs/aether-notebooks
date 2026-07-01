package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListRootContents() (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/folders", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetFolderContents(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/folders/"+id, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetFolderAncestors(id string) ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	if err := c.GetJSON("/api/v1/folders/"+id+"/ancestors", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateFolder(name, parentID string) (*Folder, error) {
	body := map[string]interface{}{"name": name}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	var f Folder
	if err := c.PostJSON("/api/v1/folders", body, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *Client) UpdateFolder(id, name string) (*Folder, error) {
	body := map[string]interface{}{"name": name}
	var f Folder
	if err := c.PutJSON("/api/v1/folders/"+id, body, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *Client) DeleteFolder(id string) error {
	return c.DeleteJSON("/api/v1/folders/" + id)
}

func (c *Client) ListHomeFolders() ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	if err := c.GetJSON("/api/v1/home", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) EnsureHomeFolder() (map[string]string, error) {
	var result map[string]string
	if err := c.PostJSON("/api/v1/users/me/home", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetRecent() ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	if err := c.GetJSON("/api/v1/recent", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func FoldersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folders",
		Short: "Manage folders",
	}

	cmd.AddCommand(
		func() *cobra.Command {
			var parent string
			c := &cobra.Command{
				Use:   "list",
				Short: "List root contents or folder contents",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					if parent != "" {
						result, err := cl.GetFolderContents(parent)
						if err != nil {
							return err
						}
						PrintJSON(result)
					} else {
						result, err := cl.ListRootContents()
						if err != nil {
							return err
						}
						PrintJSON(result)
					}
					return nil
				},
			}
			c.Flags().StringVar(&parent, "parent", "", "Folder ID to list contents of")
			return c
		}(),
		func() *cobra.Command {
			var name, parent string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a folder",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					f, err := cl.CreateFolder(name, parent)
					if err != nil {
						return err
					}
					PrintJSON(f)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Folder name (required)")
			c.MarkFlagRequired("name")
			c.Flags().StringVarP(&parent, "parent", "p", "", "Parent folder ID")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get folder contents",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetFolderContents(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Rename a folder",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					f, err := cl.UpdateFolder(args[0], name)
					if err != nil {
						return err
					}
					PrintJSON(f)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "New folder name (required)")
			c.MarkFlagRequired("name")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a folder",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteFolder(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "ancestors <id>",
			Short: "Get folder ancestor chain",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetFolderAncestors(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
	)

	return cmd
}

func HomeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "home",
		Short: "Manage home folders",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List home folders",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListHomeFolders()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "ensure",
			Short: "Ensure user home folder exists",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.EnsureHomeFolder()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
	)

	return cmd
}

func RecentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recent",
		Short: "List recently accessed items",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := LoadClient()
			if err != nil {
				return err
			}
			result, err := c.GetRecent()
			if err != nil {
				return err
			}
			PrintJSON(result)
			return nil
		},
	}
}
