package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// folderContentsPageSize is the CLI's page size while completing a listing
// section: the server's maximum, so a fully loaded folder takes the fewest
// requests.
const folderContentsPageSize = 200

// folderListingSections maps each paginated listing key to the singular type
// the load-more endpoint expects.
var folderListingSections = []struct{ key, resourceType string }{
	{"notebooks", "notebook"},
	{"connectors", "connector"},
	{"dashboards", "dashboard"},
}

// ListRootContents returns the root listing with every paginated section
// completed: the listing endpoint serves a first page per section, and any
// section reporting has_more is walked through the load-more endpoint so the
// CLI never silently truncates.
func (c *Client) ListRootContents() (map[string]interface{}, error) {
	return c.listFolderContents("/api/v1/folders", "/api/v1/folders/contents")
}

// GetFolderContents returns a folder listing with every paginated section
// completed, mirroring ListRootContents.
func (c *Client) GetFolderContents(id string) (map[string]interface{}, error) {
	return c.listFolderContents("/api/v1/folders/"+id, "/api/v1/folders/"+id+"/contents")
}

// listFolderContents loads a folder listing and completes every section that
// reports has_more, appending load-more pages until each is exhausted. The
// returned map is the server's response (same keys): only the section arrays
// grow and their has_more flags flip to false once complete.
func (c *Client) listFolderContents(listPath, contentsPath string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON(fmt.Sprintf("%s?limit=%d", listPath, folderContentsPageSize), &result); err != nil {
		return nil, err
	}
	for _, section := range folderListingSections {
		if err := c.paginateFolderSection(result, contentsPath, section.key, section.resourceType); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// paginateFolderSection appends every further page of one section to the base
// listing. A response without has_more comes from a pre-pagination server (the
// section is already complete). A server that keeps reporting has_more while
// returning no items would otherwise loop forever, so that contract violation
// fails instead of hanging.
func (c *Client) paginateFolderSection(result map[string]interface{}, contentsPath, section, resourceType string) error {
	hasMoreMap, ok := result["has_more"].(map[string]interface{})
	if !ok {
		return nil
	}
	more, _ := hasMoreMap[section].(bool)
	if !more {
		return nil
	}
	items, _ := result[section].([]interface{})
	for more {
		var page struct {
			Items   []interface{} `json:"items"`
			HasMore bool          `json:"has_more"`
		}
		path := fmt.Sprintf("%s?type=%s&offset=%d&limit=%d", contentsPath, resourceType, len(items), folderContentsPageSize)
		if err := c.GetJSON(path, &page); err != nil {
			return err
		}
		if len(page.Items) == 0 {
			return fmt.Errorf("folder listing paging stalled for %s at offset %d", section, len(items))
		}
		items = append(items, page.Items...)
		more = page.HasMore
	}
	result[section] = items
	hasMoreMap[section] = false
	return nil
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
