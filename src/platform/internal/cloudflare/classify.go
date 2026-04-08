package cloudflare

import "encoding/json"

type TokenStatus int

const (
	TokenValid      TokenStatus = iota // zone visible, #dns_records:edit present
	TokenInvalid                       // API rejected the token entirely
	TokenWrongPerms                    // valid token, missing DNS edit
	TokenWrongZone                     // valid token, can't see the zone
)

type TokenCheck struct {
	Status      TokenStatus
	Permissions []string // permissions the token has
	Message     string   // human-readable explanation
}

// ClassifyToken analyzes a Cloudflare /zones?name=<domain> response
// and determines whether the token has the required DNS edit permissions.
func ClassifyToken(responseBody []byte, domain string) TokenCheck {
	var resp zonesResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return TokenCheck{
			Status:  TokenInvalid,
			Message: "Invalid API token (could not parse response).\n\nCheck that you copied the full token value.",
		}
	}

	if !resp.Success {
		msg := "unknown error"
		if len(resp.Errors) > 0 {
			msg = resp.Errors[0].Message
		}
		return TokenCheck{
			Status:  TokenInvalid,
			Message: "Invalid API token (" + msg + ").\n\nCheck that you copied the full token value.",
		}
	}

	if len(resp.Result) == 0 {
		return TokenCheck{
			Status: TokenWrongZone,
			Message: "Token does not have access to zone \"" + domain + "\".\n\n" +
				"When creating the token, under Zone Resources select:\n" +
				"  Include → Specific zone → " + domain,
		}
	}

	// Find the zone matching the domain
	for _, zone := range resp.Result {
		if zone.Name != domain {
			continue
		}
		for _, perm := range zone.Permissions {
			if perm == "#dns_records:edit" {
				return TokenCheck{
					Status:      TokenValid,
					Permissions: zone.Permissions,
					Message:     "Valid DNS token for " + domain + ".",
				}
			}
		}
		// Has zone access but missing dns_records:edit
		return TokenCheck{
			Status:      TokenWrongPerms,
			Permissions: zone.Permissions,
			Message: "Token can access \"" + domain + "\" but lacks DNS edit permission.\n" +
				"  Current permissions: " + joinPerms(zone.Permissions) + "\n\n" +
				"Create a new token using the 'Edit zone DNS' template:\n" +
				"  1. Go to your Cloudflare profile → API Tokens\n" +
				"  2. Click 'Create Token'\n" +
				"  3. Use the 'Edit zone DNS' template\n" +
				"  4. Under Zone Resources, select: " + domain,
		}
	}

	// Zone not found in results (shouldn't happen if len > 0, but be safe)
	return TokenCheck{
		Status: TokenWrongZone,
		Message: "Token does not have access to zone \"" + domain + "\".\n\n" +
			"When creating the token, under Zone Resources select:\n" +
			"  Include → Specific zone → " + domain,
	}
}

// ClassifyTokenFromAPI fetches zones for the domain and classifies the token.
func (c *Client) ClassifyTokenFromAPI(domain string) TokenCheck {
	body, err := c.FetchZones(domain)
	if err != nil {
		return TokenCheck{
			Status:  TokenInvalid,
			Message: "Could not reach Cloudflare API: " + err.Error(),
		}
	}
	return ClassifyToken(body, domain)
}

func joinPerms(perms []string) string {
	if len(perms) == 0 {
		return "(none)"
	}
	result := perms[0]
	for _, p := range perms[1:] {
		result += ", " + p
	}
	return result
}
