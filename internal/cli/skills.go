package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListSkills() ([]Skill, error) {
	var skills []Skill
	if err := c.GetJSON("/api/v1/skills", &skills); err != nil {
		return nil, err
	}
	return skills, nil
}

func (c *Client) GetSkill(id string) (*Skill, error) {
	var s Skill
	if err := c.GetJSON("/api/v1/skills/"+id, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) CreateSkill(name, description, systemPrompt string) (*Skill, error) {
	body := map[string]interface{}{"name": name}
	if description != "" {
		body["description"] = description
	}
	if systemPrompt != "" {
		body["system_prompt"] = systemPrompt
	}
	var s Skill
	if err := c.PostJSON("/api/v1/skills", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) UpdateSkill(id, name, description, systemPrompt string) (*Skill, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if description != "" {
		body["description"] = description
	}
	if systemPrompt != "" {
		body["system_prompt"] = systemPrompt
	}
	var s Skill
	if err := c.PutJSON("/api/v1/skills/"+id, body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) DeleteSkill(id string) error {
	return c.DeleteJSON("/api/v1/skills/" + id)
}

func SkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage agent skills",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List skills",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListSkills()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, description, systemPrompt string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a skill",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					s, err := cl.CreateSkill(name, description, systemPrompt)
					if err != nil {
						return err
					}
					PrintJSON(s)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Skill name (required)")
			c.MarkFlagRequired("name")
			c.Flags().StringVarP(&description, "description", "d", "", "Skill description")
			c.Flags().StringVar(&systemPrompt, "system-prompt", "", "System prompt")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get a skill",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetSkill(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, description, systemPrompt string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update a skill",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					s, err := cl.UpdateSkill(args[0], name, description, systemPrompt)
					if err != nil {
						return err
					}
					PrintJSON(s)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Skill name")
			c.Flags().StringVarP(&description, "description", "d", "", "Skill description")
			c.Flags().StringVar(&systemPrompt, "system-prompt", "", "System prompt")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a skill",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteSkill(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
	)

	return cmd
}
