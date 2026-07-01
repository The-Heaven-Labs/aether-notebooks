package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func (c *Client) ListAttachments(notebookID string) ([]Attachment, error) {
	var atts []Attachment
	if err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/attachments", &atts); err != nil {
		return nil, err
	}
	return atts, nil
}

func (c *Client) GetAttachment(id string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/api/v1/attachments/"+id, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return nil, "", fmt.Errorf("API error %d: %s", resp.StatusCode, e.Error)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *Client) UploadAttachment(notebookID, filePath string) (*Attachment, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return nil, err
	}
	w.Close()

	req, err := http.NewRequest("POST", c.BaseURL+"/api/v1/notebooks/"+notebookID+"/attachments", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, e.Error)
	}
	var att Attachment
	if err := json.Unmarshal(data, &att); err != nil {
		return nil, err
	}
	return &att, nil
}

func (c *Client) DeleteAttachment(id string) error {
	return c.DeleteJSON("/api/v1/attachments/" + id)
}

func AttachmentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attachments",
		Short: "Manage file attachments",
	}

	cmd.AddCommand(
		func() *cobra.Command {
			var notebookID string
			c := &cobra.Command{
				Use:   "list",
				Short: "List attachments for a notebook",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := cl.ListAttachments(notebookID)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVar(&notebookID, "notebook", "", "Notebook ID (required)")
			c.MarkFlagRequired("notebook")
			return c
		}(),
		func() *cobra.Command {
			var output string
			c := &cobra.Command{
				Use:   "get <id>",
				Short: "Download an attachment",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					data, _, err := cl.GetAttachment(args[0])
					if err != nil {
						return err
					}
					dest := output
					if dest == "" {
						dest = args[0]
					}
					if err := os.WriteFile(dest, data, 0644); err != nil {
						return err
					}
					fmt.Printf("Downloaded to %s (%d bytes).\n", dest, len(data))
					return nil
				},
			}
			c.Flags().StringVarP(&output, "output", "o", "", "Output file path")
			return c
		}(),
		func() *cobra.Command {
			var notebookID, file string
			c := &cobra.Command{
				Use:   "upload",
				Short: "Upload an attachment to a notebook",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					att, err := cl.UploadAttachment(notebookID, file)
					if err != nil {
						return err
					}
					PrintJSON(att)
					return nil
				},
			}
			c.Flags().StringVar(&notebookID, "notebook", "", "Notebook ID (required)")
			c.MarkFlagRequired("notebook")
			c.Flags().StringVarP(&file, "file", "f", "", "File to upload (required)")
			c.MarkFlagRequired("file")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete an attachment",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteAttachment(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
	)

	return cmd
}
