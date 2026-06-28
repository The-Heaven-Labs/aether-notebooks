package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type Client struct {
	BaseURL string
	Token   string
}

type Credentials struct {
	Token  string `json:"token"`
	APIURL string `json:"api_url"`
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aether", "credentials.json")
}

func SaveCredentials(token, apiURL string) error {
	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(Credentials{Token: token, APIURL: apiURL}, "", "  ")
	return os.WriteFile(path, data, 0600)
}

func RemoveCredentials() {
	os.Remove(credentialsPath())
}

func LoadClient() (*Client, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, fmt.Errorf("not logged in — run 'aether login'")
	}
	var creds Credentials
	json.Unmarshal(data, &creds)
	if creds.APIURL == "" {
		creds.APIURL = "http://localhost:8080"
	}
	return &Client{BaseURL: creds.APIURL, Token: creds.Token}, nil
}

func (c *Client) Do(method, path string, body interface{}) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (c *Client) GetJSON(path string, out interface{}) error {
	data, status, err := c.Do("GET", path, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return fmt.Errorf("API error %d: %s", status, e.Error)
	}
	return json.Unmarshal(data, out)
}

func (c *Client) PostJSON(path string, body, out interface{}) error {
	data, status, err := c.Do("POST", path, body)
	if err != nil {
		return err
	}
	if status >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return fmt.Errorf("API error %d: %s", status, e.Error)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) DeleteJSON(path string) error {
	data, status, err := c.Do("DELETE", path, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return fmt.Errorf("API error %d: %s", status, e.Error)
	}
	return nil
}

func PrintJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
