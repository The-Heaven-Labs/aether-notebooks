package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func LoginCmd() *cobra.Command {
	var email, password, apiURL string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a Aether Notebooks server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				fmt.Print("Email: ")
				fmt.Scanln(&email)
			}
			if password == "" {
				fmt.Print("Password: ")
				pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				password = string(pwBytes)
				fmt.Println()
			}

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
	cmd.Flags().StringVar(&email, "email", "", "Email for login")
	cmd.Flags().StringVar(&password, "password", "", "Password for login")
	cmd.Flags().StringVar(&apiURL, "api-url", defaultAPIURL(), "API server URL")
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
