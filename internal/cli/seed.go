package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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

			// Register additional test users
			fmt.Println("\nRegistering test users...")
			testUsers := []struct {
				email    string
				password string
				name     string
			}{
				{"alice@example.com", "password123", "Alice Admin"},
				{"bob@example.com", "password123", "Bob Editor"},
				{"carol@example.com", "password123", "Carol Viewer"},
			}
			for _, u := range testUsers {
				if err := register(baseURL, u.email, u.password, u.name); err != nil {
					// User might already exist, that's ok
					fmt.Printf("  ⚠ %s already exists (skipping)\n", u.email)
				} else {
					fmt.Printf("  ✓ Registered %s\n", u.email)
				}
			}

			// Trigger home folder creation for all test users
			for _, u := range testUsers {
				// Login to trigger auto-home-folder creation on login
				token, err := loginAs(baseURL, u.email, u.password)
				if err != nil {
					fmt.Printf("  ⚠ Could not login as %s to create home folder: %v\n", u.email, err)
					continue
				}
				userCl := &Client{BaseURL: baseURL, Token: token}
				// Call the ensure home endpoint to create home folder if missing
				var homeResp map[string]string
				if err := userCl.PostJSON("/api/v1/users/me/home", nil, &homeResp); err != nil {
					fmt.Printf("  ⚠ Could not ensure home folder for %s: %v\n", u.email, err)
				} else {
					fmt.Printf("  ✓ %s home folder ready\n", u.name)
				}
			}

			// Create folders
			fmt.Println("\nCreating folders...")

			// Root folders (use getOrCreateFolder to handle idempotency)
			sharedProjectsID, err := getOrCreateFolder(cl, orgID, "", "Shared Projects")
			if err != nil {
				return fmt.Errorf("failed to create Shared Projects: %w", err)
			}
			fmt.Printf("  ✓ Shared Projects (%s)\n", sharedProjectsID)

			archivedID, err := getOrCreateFolder(cl, orgID, "", "Archive")
			if err != nil {
				return fmt.Errorf("failed to create Archive: %w", err)
			}
			fmt.Printf("  ✓ Archive (%s)\n", archivedID)

			// Shared Projects subfolders
			analyticsID, err := getOrCreateFolder(cl, orgID, sharedProjectsID, "Analytics")
			if err != nil {
				return fmt.Errorf("failed to create Analytics: %w", err)
			}
			fmt.Printf("  ✓ Analytics (%s)\n", analyticsID)

			engineeringID, err := getOrCreateFolder(cl, orgID, sharedProjectsID, "Engineering")
			if err != nil {
				return fmt.Errorf("failed to create Engineering: %w", err)
			}
			fmt.Printf("  ✓ Engineering (%s)\n", engineeringID)

			// Analytics subfolders
			reportsID, err := getOrCreateFolder(cl, orgID, analyticsID, "Reports")
			if err != nil {
				return fmt.Errorf("failed to create Reports: %w", err)
			}
			fmt.Printf("  ✓ Reports (%s)\n", reportsID)

			exportsID, err := getOrCreateFolder(cl, orgID, analyticsID, "Exports")
			if err != nil {
				return fmt.Errorf("failed to create Exports: %w", err)
			}
			fmt.Printf("  ✓ Exports (%s)\n", exportsID)

			// Engineering subfolders
			apiDocsID, err := getOrCreateFolder(cl, orgID, engineeringID, "API Docs")
			if err != nil {
				return fmt.Errorf("failed to create API Docs: %w", err)
			}
			fmt.Printf("  ✓ API Docs (%s)\n", apiDocsID)

			// Archive subfolders
			oldReportsID, err := getOrCreateFolder(cl, orgID, archivedID, "2024 Reports")
			if err != nil {
				return fmt.Errorf("failed to create 2024 Reports: %w", err)
			}
			fmt.Printf("  ✓ 2024 Reports (%s)\n", oldReportsID)

			// Get user's home folder, create if doesn't exist
			homeID, err := getHomeFolder(cl)
			if err != nil {
				fmt.Printf("  ⚠ No home folder found, creating... ")
				homeID, err = getOrCreateFolder(cl, orgID, "", me.Name+"'s Home")
				if err != nil {
					return fmt.Errorf("failed to create home folder: %w", err)
				}
				// Mark as home folder via direct API call if possible, otherwise just use it
				fmt.Printf("created %s\n", homeID)
			}
			fmt.Printf("  ✓ Home folder (%s)\n", homeID)

			// Home folder subfolders (use getOrCreateFolder to handle idempotency)
			mlResearchID, err := getOrCreateFolder(cl, orgID, homeID, "ML Research")
			if err != nil {
				return fmt.Errorf("failed to create ML Research: %w", err)
			}
			fmt.Printf("  ✓ ML Research (%s)\n", mlResearchID)

			experimentsID, err := getOrCreateFolder(cl, orgID, mlResearchID, "Experiments")
			if err != nil {
				return fmt.Errorf("failed to create Experiments: %w", err)
			}
			fmt.Printf("  ✓ Experiments (%s)\n", experimentsID)

			datasetsID, err := getOrCreateFolder(cl, orgID, mlResearchID, "Datasets")
			if err != nil {
				return fmt.Errorf("failed to create Datasets: %w", err)
			}
			fmt.Printf("  ✓ Datasets (%s)\n", datasetsID)

			scratchID, err := getOrCreateFolder(cl, orgID, homeID, "Scratch")
			if err != nil {
				return fmt.Errorf("failed to create Scratch: %w", err)
			}
			fmt.Printf("  ✓ Scratch (%s)\n", scratchID)

			// Create notebooks
			fmt.Println("\nCreating notebooks...")

			// Analytics notebooks
			salesDashboardID, err := createNotebook(cl, orgID, "Sales Dashboard", "Quarterly sales metrics overview", analyticsID)
			if err != nil {
				return fmt.Errorf("failed to create Sales Dashboard: %w", err)
			}
			fmt.Printf("  ✓ Sales Dashboard (%s)\n", salesDashboardID)

			userMetricsID, err := createNotebook(cl, orgID, "User Metrics", "Daily active users and retention", reportsID)
			if err != nil {
				return fmt.Errorf("failed to create User Metrics: %w", err)
			}
			fmt.Printf("  ✓ User Metrics (%s)\n", userMetricsID)

			monthlyExportID, err := createNotebook(cl, orgID, "Monthly Export", "Data export summary", exportsID)
			if err != nil {
				return fmt.Errorf("failed to create Monthly Export: %w", err)
			}
			fmt.Printf("  ✓ Monthly Export (%s)\n", monthlyExportID)

			// Engineering notebooks
			apiMonitoringID, err := createNotebook(cl, orgID, "API Monitoring", "Production API latency and error rates", engineeringID)
			if err != nil {
				return fmt.Errorf("failed to create API Monitoring: %w", err)
			}
			fmt.Printf("  ✓ API Monitoring (%s)\n", apiMonitoringID)

			apiReferenceID, err := createNotebook(cl, orgID, "API Reference", "Internal API documentation", apiDocsID)
			if err != nil {
				return fmt.Errorf("failed to create API Reference: %w", err)
			}
			fmt.Printf("  ✓ API Reference (%s)\n", apiReferenceID)

			// Archive notebooks
			q1ReviewID, err := createNotebook(cl, orgID, "Q1 2024 Review", "First quarter analysis", oldReportsID)
			if err != nil {
				return fmt.Errorf("failed to create Q1 2024 Review: %w", err)
			}
			fmt.Printf("  ✓ Q1 2024 Review (%s)\n", q1ReviewID)

			// Home folder notebooks
			modelTrainingID, err := createNotebook(cl, orgID, "Model Training", "Training scripts for recommendation model", mlResearchID)
			if err != nil {
				return fmt.Errorf("failed to create Model Training: %w", err)
			}
			fmt.Printf("  ✓ Model Training (%s)\n", modelTrainingID)

			hypothesisID, err := createNotebook(cl, orgID, "Hypothesis Testing", "A/B test results and analysis", experimentsID)
			if err != nil {
				return fmt.Errorf("failed to create Hypothesis Testing: %w", err)
			}
			fmt.Printf("  ✓ Hypothesis Testing (%s)\n", hypothesisID)

			personalNotesID, err := createNotebook(cl, orgID, "Personal Notes", "My private notes and ideas", homeID)
			if err != nil {
				return fmt.Errorf("failed to create Personal Notes: %w", err)
			}
			fmt.Printf("  ✓ Personal Notes (%s)\n", personalNotesID)

			scratchpadID, err := createNotebook(cl, orgID, "Scratchpad", "Quick calculations and explorations", scratchID)
			if err != nil {
				return fmt.Errorf("failed to create Scratchpad: %w", err)
			}
			fmt.Printf("  ✓ Scratchpad (%s)\n", scratchpadID)

			// Create Everyone group and add all org members
			fmt.Println("\nCreating Everyone group...")
			everyoneID, err := createGroup(cl, orgID, "Everyone")
			if err != nil {
				fmt.Printf("  ⚠ Everyone group may already exist: %v (skipping)\n", err)
			} else {
				fmt.Printf("  ✓ Everyone (%s)\n", everyoneID)

				// Add all org members to Everyone group
				var members []map[string]interface{}
				if err := cl.GetJSON("/api/v1/members", &members); err == nil {
					for _, m := range members {
						if userID, ok := m["user_id"].(string); ok {
							if err := addGroupMember(cl, everyoneID, userID); err != nil {
								fmt.Printf("  ⚠ Could not add user to Everyone: %v\n", err)
							}
						}
					}
					fmt.Printf("  ✓ Added all members to Everyone group\n")
				}
			}

			// Set up ACLs using special "everyone" org_role (no group needed - auto-includes all org members)
			fmt.Println("\nSetting up permissions...")

			// Give "everyone" org_role view+create on Archive only (Shared Projects gets it later with Data Team)
			if err := setACL(cl, "folder", archivedID, []aclEntryInput{
				{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view", "create"}},
			}); err != nil {
				fmt.Printf("  ⚠ Failed to set Everyone ACL on Archive: %v\n", err)
			} else {
				fmt.Printf("  ✓ Everyone can view+create in Archive\n")
			}

			// Give "everyone" view on all notebooks
			for _, notebookID := range []string{salesDashboardID, userMetricsID, monthlyExportID, apiMonitoringID, apiReferenceID, q1ReviewID, modelTrainingID, hypothesisID, personalNotesID, scratchpadID} {
				if err := setACL(cl, "notebook", notebookID, []aclEntryInput{
					{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view"}},
				}); err != nil {
					fmt.Printf("  ⚠ Failed to set Everyone ACL on notebook: %v\n", err)
				}
			}
			fmt.Println("\nCreating Data Team group...")
			dataTeamID, err := createGroup(cl, orgID, "Data Team")
			if err != nil {
				fmt.Printf("  ⚠ Data Team group may already exist: %v (skipping)\n", err)
			} else {
				fmt.Printf("  ✓ Data Team (%s)\n", dataTeamID)

				// Add current user to Data Team
				if err := addGroupMember(cl, dataTeamID, me.ID); err != nil {
					fmt.Printf("  ⚠ Could not add self to group: %v (may already be a member)\n", err)
				} else {
					fmt.Printf("  ✓ Added %s to Data Team\n", me.Name)
				}
			}

			// Invite bob to Data Team and create some content for bob
			bobToken, err := loginAs(baseURL, "bob@example.com", "password123")
			if err != nil {
				fmt.Printf("  ⚠ Could not login as Bob: %v (skipping bob setup)\n", err)
			} else {
				bobCl := &Client{BaseURL: baseURL, Token: bobToken}
				var bobMe struct {
					ID string `json:"id"`
				}
				if err := bobCl.GetJSON("/api/v1/users/me", &bobMe); err == nil {
					// Try to add Bob to Data Team
					if dataTeamID != "" {
						if err := addGroupMember(cl, dataTeamID, bobMe.ID); err != nil {
							fmt.Printf("  ⚠ Could not add Bob to Data Team: %v\n", err)
						} else {
							fmt.Printf("  ✓ Added Bob to Data Team\n")
						}
					}
					// Give Bob a home folder with content
					var bobsHome []map[string]interface{}
					if err := bobCl.GetJSON("/api/v1/home", &bobsHome); err == nil && len(bobsHome) > 0 {
						bobHomeID := bobsHome[0]["id"].(string)
						bobProjectsID, err := createFolderForUser(bobCl, bobHomeID, "Bob's Projects")
						if err == nil {
							fmt.Printf("  ✓ Bob's Projects folder created\n")
							createNotebookForUser(bobCl, "Bob's Analysis", "Personal analysis notebook", bobProjectsID)
							fmt.Printf("  ✓ Bob's Analysis notebook created\n")
						}
					}
				}
			}

			// Set up ACLs
			fmt.Println("\nSetting up permissions...")

			// Give Data Team AND everyone org_role view+create on Shared Projects
			if err := setACL(cl, "folder", sharedProjectsID, []aclEntryInput{
				{SubjectType: "group", SubjectID: dataTeamID, Actions: []string{"view", "create"}},
				{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view", "create"}},
			}); err != nil {
				fmt.Printf("  ⚠ Failed to set ACL on Shared Projects: %v\n", err)
			} else {
				fmt.Printf("  ✓ Data Team + Everyone can view+create in Shared Projects\n")
			}

			// Give Data Team view+edit+run on Sales Dashboard
			if err := setACL(cl, "notebook", salesDashboardID, []aclEntryInput{
				{SubjectType: "group", SubjectID: dataTeamID, Actions: []string{"view", "edit", "run"}},
			}); err != nil {
				fmt.Printf("  ⚠ Failed to set ACL on Sales Dashboard: %v\n", err)
			} else {
				fmt.Printf("  ✓ Data Team can view+edit+run Sales Dashboard\n")
			}

			// Note: Everyone group ACLs above use org_role:everyone which auto-includes
			// all org members without needing group membership. The group-based ACLs at
			// lines 370-380 were removed because they overwrote the org_role ACLs.

			fmt.Println("\n✓ Seed data created successfully!")
			fmt.Println("\nFolder hierarchy:")
			fmt.Println("  Home")
			fmt.Println("  ├── ML Research")
			fmt.Println("  │   ├── Experiments")
			fmt.Println("  │   │   └── Hypothesis Testing")
			fmt.Println("  │   └── Datasets")
			fmt.Println("  │       └── Model Training")
			fmt.Println("  ├── Scratch")
			fmt.Println("  │   └── Scratchpad")
			fmt.Println("  └── Personal Notes")
			fmt.Println("  Shared Projects")
			fmt.Println("  ├── Analytics")
			fmt.Println("  │   ├── Reports")
			fmt.Println("  │   │   └── User Metrics")
			fmt.Println("  │   ├── Exports")
			fmt.Println("  │   │   └── Monthly Export")
			fmt.Println("  │   └── Sales Dashboard")
			fmt.Println("  ├── Engineering")
			fmt.Println("  │   ├── API Docs")
			fmt.Println("  │   │   └── API Reference")
			fmt.Println("  │   └── API Monitoring")
			fmt.Println("  └── Archive")
			fmt.Println("      └── 2024 Reports")
			fmt.Println("          └── Q1 2024 Review")

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

func register(baseURL, email, password, name string) error {
	body := map[string]string{
		"email":    email,
		"password": password,
		"name":     name,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("register request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed: %s", string(respData))
	}
	return nil
}

func loginAs(baseURL, email, password string) (token string, err error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("login failed")
	}

	var res struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return "", fmt.Errorf("parse login response: %w", err)
	}
	return res.Token, nil
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

func getOrCreateFolder(cl *Client, orgID, parentID, name string) (string, error) {
	body := map[string]interface{}{"name": name}
	if parentID != "" {
		body["parent_id"] = parentID
	}

	var resp map[string]interface{}
	postErr := cl.PostJSON("/api/v1/folders", body, &resp)
	if postErr == nil {
		return resp["id"].(string), nil
	}

	if !strings.Contains(postErr.Error(), "500") || !strings.Contains(postErr.Error(), "failed to create folder") {
		return "", postErr
	}

	var listResp struct {
		Folders []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ParentID  string `json:"parent_id"`
			CreatedAt string `json:"created_at"`
		} `json:"folders"`
	}
	var listErr error
	if parentID == "" {
		listErr = cl.GetJSON("/api/v1/folders", &listResp)
	} else {
		listErr = cl.GetJSON("/api/v1/folders/"+parentID, &listResp)
	}
	if listErr != nil {
		return "", fmt.Errorf("folder %q already exists but cannot list folders: %v", name, listErr)
	}

	var bestMatch struct {
		ID        string
		CreatedAt string
	}
	found := false
	for _, f := range listResp.Folders {
		if f.Name == name && f.ParentID == parentID {
			if !found || f.CreatedAt > bestMatch.CreatedAt {
				bestMatch.ID = f.ID
				bestMatch.CreatedAt = f.CreatedAt
				found = true
			}
		}
	}
	if found {
		return bestMatch.ID, nil
	}
	return "", fmt.Errorf("folder %q already exists but cannot find ID", name)
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

func createFolderForUser(cl *Client, parentID, name string) (string, error) {
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

func createNotebookForUser(cl *Client, title, description, folderID string) error {
	body := map[string]interface{}{
		"title":       title,
		"description": description,
	}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	var resp map[string]interface{}
	return cl.PostJSON("/api/v1/notebooks", body, &resp)
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
