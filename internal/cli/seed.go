package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

func SeedCmd() *cobra.Command {
	var email, password, apiURL string

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed demo data (folders, notebooks, groups) to showcase the folder hierarchy",
		Long: `Seeds demo data into the current org to demonstrate folder hierarchy and permissions.

Example:
  hnb seed --email demo@example.com --password secret

This creates:
- Folders: Shared Projects > Analytics, Shared Projects > Engineering, Angel Home > ML Research
- Notebooks: Sales Dashboard (Analytics), API Monitoring (Engineering), Model Training (ML Research), Personal Notes (Angel Home)
- Groups: Data Team (with current user as member)
- ACLs: Cross-user permissions to demonstrate inheritance and specificity
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" || password == "" {
				return fmt.Errorf("--email and --password are required")
			}

			baseURL := apiURL
			if baseURL == "" {
				baseURL = "http://localhost:8080"
			}

			// Login and get token
			token, orgID, err := login(baseURL, email, password)
			if err != nil {
				return err
			}
			fmt.Printf("Logged in as %s\n", email)

			cl := &Client{BaseURL: baseURL, Token: token}

			// Get current user info
			var me struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := cl.GetJSON("/api/v1/users/me", &me); err != nil {
				return fmt.Errorf("failed to get current user: %w", err)
			}
			fmt.Printf("User ID: %s, Name: %s, Org: %s\n", me.ID, me.Name, orgID)

			// Create folders
			fmt.Println("\nCreating folders...")
			sharedProjectsID, err := createFolder(cl, orgID, "", "Shared Projects")
			if err != nil {
				return fmt.Errorf("failed to create Shared Projects: %w", err)
			}
			fmt.Printf("  ✓ Shared Projects (%s)\n", sharedProjectsID)

			analyticsID, err := createFolder(cl, orgID, sharedProjectsID, "Analytics")
			if err != nil {
				return fmt.Errorf("failed to create Analytics: %w", err)
			}
			fmt.Printf("  ✓ Analytics (%s)\n", analyticsID)

			engineeringID, err := createFolder(cl, orgID, sharedProjectsID, "Engineering")
			if err != nil {
				return fmt.Errorf("failed to create Engineering: %w", err)
			}
			fmt.Printf("  ✓ Engineering (%s)\n", engineeringID)

			// Get user's home folder
			homeID, err := getHomeFolder(cl)
			if err != nil {
				return fmt.Errorf("failed to get home folder: %w", err)
			}
			fmt.Printf("  ✓ Home folder (%s)\n", homeID)

			mlResearchID, err := createFolder(cl, orgID, homeID, "ML Research")
			if err != nil {
				return fmt.Errorf("failed to create ML Research: %w", err)
			}
			fmt.Printf("  ✓ ML Research (%s)\n", mlResearchID)

			// Create notebooks
			fmt.Println("\nCreating notebooks...")
			salesDashboardID, err := createNotebook(cl, orgID, "Sales Dashboard", "Quarterly sales metrics overview", analyticsID)
			if err != nil {
				return fmt.Errorf("failed to create Sales Dashboard: %w", err)
			}
			fmt.Printf("  ✓ Sales Dashboard (%s)\n", salesDashboardID)

			apiMonitoringID, err := createNotebook(cl, orgID, "API Monitoring", "Production API latency and error rates", engineeringID)
			if err != nil {
				return fmt.Errorf("failed to create API Monitoring: %w", err)
			}
			fmt.Printf("  ✓ API Monitoring (%s)\n", apiMonitoringID)

			modelTrainingID, err := createNotebook(cl, orgID, "Model Training", "Training scripts for recommendation model", mlResearchID)
			if err != nil {
				return fmt.Errorf("failed to create Model Training: %w", err)
			}
			fmt.Printf("  ✓ Model Training (%s)\n", modelTrainingID)

			personalNotesID, err := createNotebook(cl, orgID, "Personal Notes", "My private notes and ideas", homeID)
			if err != nil {
				return fmt.Errorf("failed to create Personal Notes: %w", err)
			}
			fmt.Printf("  ✓ Personal Notes (%s)\n", personalNotesID)

			// Create Data Team group
			fmt.Println("\nCreating Data Team group...")
			dataTeamID, err := createGroup(cl, orgID, "Data Team")
			if err != nil {
				return fmt.Errorf("failed to create Data Team group: %w", err)
			}
			fmt.Printf("  ✓ Data Team (%s)\n", dataTeamID)

			// Add current user to Data Team
			if err := addGroupMember(cl, dataTeamID, me.ID); err != nil {
				fmt.Printf("  ⚠ Could not add self to group: %v (may already be a member)\n", err)
			} else {
				fmt.Printf("  ✓ Added %s to Data Team\n", me.Name)
			}

			// Set up ACLs
			fmt.Println("\nSetting up permissions...")

			// Give Data Team view+create on Shared Projects
			if err := setACL(cl, "folder", sharedProjectsID, []aclEntryInput{
				{SubjectType: "group", SubjectID: dataTeamID, Actions: []string{"view", "create"}},
			}); err != nil {
				fmt.Printf("  ⚠ Failed to set ACL on Shared Projects: %v\n", err)
			} else {
				fmt.Printf("  ✓ Data Team can view+create in Shared Projects\n")
			}

			// Give Data Team view+edit+run on Sales Dashboard
			if err := setACL(cl, "notebook", salesDashboardID, []aclEntryInput{
				{SubjectType: "group", SubjectID: dataTeamID, Actions: []string{"view", "edit", "run"}},
			}); err != nil {
				fmt.Printf("  ⚠ Failed to set ACL on Sales Dashboard: %v\n", err)
			} else {
				fmt.Printf("  ✓ Data Team can view+edit+run Sales Dashboard\n")
			}

			fmt.Println("\n✓ Seed data created successfully!")
			fmt.Println("\nFolder hierarchy:")
			fmt.Println("  Angel Home / ML Research")
			fmt.Println("    ├── Model Training")
			fmt.Println("    └── Personal Notes")
			fmt.Println("  Shared Projects")
			fmt.Println("    ├── Analytics")
			fmt.Println("    │   └── Sales Dashboard")
			fmt.Println("    └── Engineering")
			fmt.Println("        └── API Monitoring")
			fmt.Println("  Data Team group: members can view+create in Shared Projects")
			fmt.Println("  Data Team group: members can view+edit+run Sales Dashboard")

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email for login (required)")
	cmd.Flags().StringVar(&password, "password", "", "Password for login (required)")
	cmd.Flags().StringVar(&apiURL, "api-url", "http://localhost:8080", "API server URL")
	cmd.MarkFlagRequired("email")
	cmd.MarkFlagRequired("password")

	return cmd
}

func login(apiURL, email, password string) (token, orgID string, err error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(apiURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return "", "", fmt.Errorf("login failed: %s", e.Error)
	}

	var res struct {
		Token string `json:"token"`
		Org   struct {
			ID string `json:"id"`
		} `json:"org"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return "", "", fmt.Errorf("parse login response: %w", err)
	}
	return res.Token, res.Org.ID, nil
}

func createFolder(cl *Client, orgID, parentID, name string) (string, error) {
	body := map[string]interface{}{"name": name}
	if parentID != "" {
		body["parent_id"] = parentID
	}

	var resp map[string]interface{}
	if err := cl.PostJSON("/api/v1/folders", body, &resp); err != nil {
		return "", err
	}
	return resp["id"].(string), nil
}

func createNotebook(cl *Client, orgID, title, description, folderID string) (string, error) {
	body := map[string]interface{}{
		"title":       title,
		"description": description,
	}
	if folderID != "" {
		body["folder_id"] = folderID
	}

	var resp map[string]interface{}
	if err := cl.PostJSON("/api/v1/notebooks", body, &resp); err != nil {
		return "", err
	}
	return resp["id"].(string), nil
}

func createGroup(cl *Client, orgID, name string) (string, error) {
	body := map[string]string{"name": name}
	var resp struct {
		ID string `json:"id"`
	}
	if err := cl.PostJSON("/api/v1/groups", body, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

func addGroupMember(cl *Client, groupID, userID string) error {
	body := map[string]string{"user_id": userID}
	return cl.PostJSON("/api/v1/groups/"+groupID+"/members", body, nil)
}

func getHomeFolder(cl *Client) (string, error) {
	var resp []map[string]interface{}
	if err := cl.GetJSON("/api/v1/home", &resp); err != nil {
		return "", err
	}
	if len(resp) == 0 {
		return "", fmt.Errorf("no home folders found")
	}
	me := map[string]interface{}{}
	if err := cl.GetJSON("/api/v1/users/me", &me); err != nil {
		return "", err
	}
	userID := me["id"].(string)

	for _, entry := range resp {
		if ownerID, ok := entry["owner_id"].(string); ok && ownerID == userID {
			return entry["id"].(string), nil
		}
	}
	return "", fmt.Errorf("home folder not found for current user")
}

type aclEntryInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
}

func setACL(cl *Client, resourceType, resourceID string, entries []aclEntryInput) error {
	body := map[string]interface{}{"entries": entries}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", cl.BaseURL+"/api/v1/acl/"+resourceType+"/"+resourceID, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cl.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ACL PUT failed: %s", string(respData))
	}
	return nil
}
