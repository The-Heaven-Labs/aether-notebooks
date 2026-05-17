package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func LoginCmd() *cobra.Command {
	var apiURL string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a Heaven's Notebooks server",
		RunE: func(cmd *cobra.Command, args []string) error {
			var email, password string
			fmt.Print("Email: ")
			fmt.Scanln(&email)
			fmt.Print("Password: ")
			fmt.Scanln(&password)

			body, _ := json.Marshal(map[string]string{"email": email, "password": password})
			resp, err := http.Post(apiURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 {
				var e struct {
					Error string `json:"error"`
				}
				json.Unmarshal(data, &e)
				return fmt.Errorf("login failed: %s", e.Error)
			}
			var res struct {
				Token string `json:"token"`
			}
			json.Unmarshal(data, &res)
			if err := SaveCredentials(res.Token, apiURL); err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "Logged in successfully.")
			return nil
		},
	}
	cmd.Flags().StringVar(&apiURL, "api-url", "http://localhost:8080", "API server URL")
	return cmd
}

func LogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		Run: func(cmd *cobra.Command, args []string) {
			RemoveCredentials()
			fmt.Println("Logged out.")
		},
	}
}
