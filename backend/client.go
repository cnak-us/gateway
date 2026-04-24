package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a lightweight HTTP client for the CNAK backend API.
type Client struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

// userResponse represents the JSON response from the backend user endpoint.
type userResponse struct {
	ID       string          `json:"id"`
	Username string          `json:"username"`
	Groups   []groupResponse `json:"groups"`
}

type groupResponse struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
}

// NewClient creates a new backend API client.
func NewClient(backendURL, serviceToken string) *Client {
	return &Client{
		baseURL:      backendURL,
		serviceToken: serviceToken,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// groupListEntry represents a group from the GET /api/groups response.
type groupListEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateUser creates a user in the backend. Returns the new user's ID.
// If the user already exists (409/400), it looks up the existing user.
func (c *Client) CreateUser(username string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"username":    username,
		"displayName": username,
		"role":        "user",
		"authType":    "certificate",
	})

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/users", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("creating user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var created struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return "", fmt.Errorf("decoding created user: %w", err)
		}
		return created.ID, nil
	}

	// User may already exist — look up by username
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict {
		return c.lookupUserID(username)
	}

	return "", fmt.Errorf("unexpected status %d from backend", resp.StatusCode)
}

// lookupUserID retrieves the user ID for a given username.
func (c *Client) lookupUserID(username string) (string, error) {
	u := fmt.Sprintf("%s/api/users/by-username/%s", c.baseURL, url.PathEscape(username))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("looking up user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("user lookup returned status %d", resp.StatusCode)
	}

	var user userResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("decoding user: %w", err)
	}
	return user.ID, nil
}

// AddUserToGroups adds the user to each of the named groups with direction "both".
// It fetches the group list once to resolve names → IDs, then adds the user to each.
func (c *Client) AddUserToGroups(userID string, groupNames []string) error {
	if len(groupNames) == 0 {
		return nil
	}

	// Fetch all groups to resolve names to IDs
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/groups", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching groups: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching groups returned status %d", resp.StatusCode)
	}

	var groups []groupListEntry
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return fmt.Errorf("decoding groups: %w", err)
	}

	nameToID := make(map[string]string, len(groups))
	for _, g := range groups {
		nameToID[g.Name] = g.ID
	}

	for _, name := range groupNames {
		gid, ok := nameToID[name]
		if !ok {
			continue // skip unknown group names
		}
		if err := c.addMember(gid, userID); err != nil {
			return fmt.Errorf("adding user to group %q: %w", name, err)
		}
	}
	return nil
}

func (c *Client) addMember(groupID, userID string) error {
	body, _ := json.Marshal(map[string]string{
		"userId":    userID,
		"direction": "both",
	})

	u := fmt.Sprintf("%s/api/groups/%s/members", c.baseURL, url.PathEscape(groupID))
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// GetAllPoints fetches all current points from the backend API.
// Returns the raw JSON response body for the caller to unmarshal.
func (c *Client) GetAllPoints() ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/points", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching points: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching points returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return data, nil
}

// GetUserGroups looks up a user by username and returns the group names
// where the direction is "out" or "both". Returns nil, nil if the user
// is not found (404).
func (c *Client) GetUserGroups(username string) ([]string, error) {
	u := fmt.Sprintf("%s/api/users/by-username/%s", c.baseURL, url.PathEscape(username))

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting user groups: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from backend", resp.StatusCode)
	}

	var user userResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding user response: %w", err)
	}

	var groups []string
	for _, g := range user.Groups {
		if g.Direction == "out" || g.Direction == "both" {
			groups = append(groups, g.Name)
		}
	}
	return groups, nil
}
