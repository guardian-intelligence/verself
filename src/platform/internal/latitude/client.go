package latitude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultBaseURL = "https://api.latitude.sh"

// Client talks to the Latitude.sh API.
type Client struct {
	Token   string
	BaseURL string // override for testing; empty = production
	HTTP    *http.Client
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) do(method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL()+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return data, fmt.Errorf("authentication failed (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return data, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, data)
	}

	return data, nil
}

// User is a Latitude.sh user from /user/profile.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type userResponse struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			Email string `json:"email"`
		} `json:"attributes"`
	} `json:"data"`
}

// ValidateToken checks the API token by fetching the current user.
func (c *Client) ValidateToken() (*User, error) {
	data, err := c.do("GET", "/user/profile", nil)
	if err != nil {
		return nil, err
	}
	var resp userResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse user response: %w", err)
	}
	return &User{
		ID:    resp.Data.ID,
		Email: resp.Data.Attributes.Email,
	}, nil
}

// Project is a Latitude.sh project.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type projectsResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"attributes"`
	} `json:"data"`
}

// ListProjects returns all projects visible to the token.
func (c *Client) ListProjects() ([]Project, error) {
	data, err := c.do("GET", "/projects", nil)
	if err != nil {
		return nil, err
	}
	var resp projectsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse projects: %w", err)
	}
	projects := make([]Project, len(resp.Data))
	for i, d := range resp.Data {
		projects[i] = Project{
			ID:   d.ID,
			Name: d.Attributes.Name,
			Slug: d.Attributes.Slug,
		}
	}
	return projects, nil
}

// Region is a Latitude.sh datacenter site.
type Region struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Country string `json:"country"`
}

type regionsResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Name    string `json:"name"`
			Slug    string `json:"slug"`
			Country struct {
				Name string `json:"name"`
			} `json:"country"`
		} `json:"attributes"`
	} `json:"data"`
}

// ListRegions returns available datacenter regions.
func (c *Client) ListRegions() ([]Region, error) {
	data, err := c.do("GET", "/regions", nil)
	if err != nil {
		return nil, err
	}
	var resp regionsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse regions: %w", err)
	}
	regions := make([]Region, len(resp.Data))
	for i, d := range resp.Data {
		regions[i] = Region{
			ID:      d.ID,
			Name:    d.Attributes.Name,
			Slug:    d.Attributes.Slug,
			Country: d.Attributes.Country.Name,
		}
	}
	return regions, nil
}

// Plan is a Latitude.sh server plan.
type Plan struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type plansResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Name    string `json:"name"`
			Slug    string `json:"slug"`
			Regions []struct {
				Locations struct {
					Available []string `json:"available"`
				} `json:"locations"`
			} `json:"regions"`
		} `json:"attributes"`
	} `json:"data"`
}

// ListPlans returns available server plans. If region is non-empty,
// only plans available in that region slug are returned.
func (c *Client) ListPlans(region string) ([]Plan, error) {
	data, err := c.do("GET", "/plans", nil)
	if err != nil {
		return nil, err
	}
	var resp plansResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse plans: %w", err)
	}
	var plans []Plan
	for _, d := range resp.Data {
		if region != "" && !planAvailableIn(d.Attributes.Regions, region) {
			continue
		}
		plans = append(plans, Plan{
			ID:   d.ID,
			Name: d.Attributes.Name,
			Slug: d.Attributes.Slug,
		})
	}
	return plans, nil
}

func planAvailableIn(regions []struct {
	Locations struct {
		Available []string `json:"available"`
	} `json:"locations"`
}, slug string,
) bool {
	for _, r := range regions {
		for _, loc := range r.Locations.Available {
			if loc == slug {
				return true
			}
		}
	}
	return false
}
