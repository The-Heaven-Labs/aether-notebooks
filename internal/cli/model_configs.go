package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListModelConfigs() ([]ModelConfig, error) {
	var configs []ModelConfig
	if err := c.GetJSON("/api/v1/model_configs", &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

func (c *Client) GetModelConfig(id string) (*ModelConfig, error) {
	var mc ModelConfig
	if err := c.GetJSON("/api/v1/model_configs/"+id, &mc); err != nil {
		return nil, err
	}
	return &mc, nil
}

func (c *Client) CreateModelConfig(name, provider, baseURL, model string) (*ModelConfig, error) {
	body := map[string]interface{}{
		"name":     name,
		"provider": provider,
		"base_url": baseURL,
		"model":    model,
	}
	var mc ModelConfig
	if err := c.PostJSON("/api/v1/model_configs", body, &mc); err != nil {
		return nil, err
	}
	return &mc, nil
}

func (c *Client) UpdateModelConfig(id, name, provider, baseURL, model string) (*ModelConfig, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if provider != "" {
		body["provider"] = provider
	}
	if baseURL != "" {
		body["base_url"] = baseURL
	}
	if model != "" {
		body["model"] = model
	}
	var mc ModelConfig
	if err := c.PutJSON("/api/v1/model_configs/"+id, body, &mc); err != nil {
		return nil, err
	}
	return &mc, nil
}

func (c *Client) DeleteModelConfig(id string) error {
	return c.DeleteJSON("/api/v1/model_configs/" + id)
}

func (c *Client) TestModelConfig(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.PostJSON("/api/v1/model_configs/"+id+"/test", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func ModelConfigsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model-configs",
		Short: "Manage model configurations",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List model configs",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListModelConfigs()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, provider, baseURL, model string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a model config",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					mc, err := cl.CreateModelConfig(name, provider, baseURL, model)
					if err != nil {
						return err
					}
					PrintJSON(mc)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Config name (required)")
			c.MarkFlagRequired("name")
			c.Flags().StringVar(&provider, "provider", "", "Provider (required)")
			c.MarkFlagRequired("provider")
			c.Flags().StringVar(&baseURL, "base-url", "", "Base URL")
			c.Flags().StringVar(&model, "model", "", "Model name (required)")
			c.MarkFlagRequired("model")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get a model config",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetModelConfig(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, provider, baseURL, model string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update a model config",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					mc, err := cl.UpdateModelConfig(args[0], name, provider, baseURL, model)
					if err != nil {
						return err
					}
					PrintJSON(mc)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Config name")
			c.Flags().StringVar(&provider, "provider", "", "Provider")
			c.Flags().StringVar(&baseURL, "base-url", "", "Base URL")
			c.Flags().StringVar(&model, "model", "", "Model name")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a model config",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteModelConfig(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "test <id>",
			Short: "Test a model config",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.TestModelConfig(args[0])
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
