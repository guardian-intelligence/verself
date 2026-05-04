package jmap

import (
	"encoding/json"
	"fmt"
)

const (
	CoreCapability = "urn:ietf:params:jmap:core"
	MailCapability = "urn:ietf:params:jmap:mail"
)

type Session struct {
	Accounts       map[string]SessionAccount `json:"accounts"`
	APIURL         string                    `json:"apiUrl"`
	EventSourceURL string                    `json:"eventSourceUrl"`
	State          string                    `json:"state"`
}

type SessionAccount struct {
	Name string `json:"name"`
}

type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type BodyPart struct {
	PartID string `json:"partId"`
	BlobID string `json:"blobId"`
	Type   string `json:"type"`
}

type BodyValue struct {
	IsEncodingProblem bool   `json:"isEncodingProblem"`
	IsTruncated       bool   `json:"isTruncated"`
	Value             string `json:"value"`
}

type Mailbox struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ParentID      string `json:"parentId"`
	Role          string `json:"role"`
	SortOrder     int    `json:"sortOrder"`
	IsSubscribed  bool   `json:"isSubscribed"`
	TotalEmails   int    `json:"totalEmails"`
	UnreadEmails  int    `json:"unreadEmails"`
	TotalThreads  int    `json:"totalThreads"`
	UnreadThreads int    `json:"unreadThreads"`
}

type Email struct {
	ID            string               `json:"id"`
	BlobID        string               `json:"blobId"`
	ThreadID      string               `json:"threadId"`
	MailboxIDs    map[string]bool      `json:"mailboxIds"`
	Keywords      map[string]bool      `json:"keywords"`
	ReceivedAt    string               `json:"receivedAt"`
	SentAt        string               `json:"sentAt"`
	Subject       string               `json:"subject"`
	From          []Address            `json:"from"`
	To            []Address            `json:"to"`
	Cc            []Address            `json:"cc"`
	ReplyTo       []Address            `json:"replyTo"`
	Preview       string               `json:"preview"`
	HasAttachment bool                 `json:"hasAttachment"`
	Size          int                  `json:"size"`
	TextBody      []BodyPart           `json:"textBody"`
	HTMLBody      []BodyPart           `json:"htmlBody"`
	BodyValues    map[string]BodyValue `json:"bodyValues"`
}

type Thread struct {
	ID       string   `json:"id"`
	EmailIDs []string `json:"emailIds"`
}

type MailboxGetResult struct {
	AccountID string    `json:"accountId"`
	State     string    `json:"state"`
	List      []Mailbox `json:"list"`
	NotFound  []string  `json:"notFound"`
}

type EmailQueryResult struct {
	AccountID           string   `json:"accountId"`
	QueryState          string   `json:"queryState"`
	CanCalculateChanges bool     `json:"canCalculateChanges"`
	Position            int      `json:"position"`
	IDs                 []string `json:"ids"`
}

type EmailGetResult struct {
	AccountID string   `json:"accountId"`
	State     string   `json:"state"`
	List      []Email  `json:"list"`
	NotFound  []string `json:"notFound"`
}

type ThreadGetResult struct {
	AccountID string   `json:"accountId"`
	State     string   `json:"state"`
	List      []Thread `json:"list"`
	NotFound  []string `json:"notFound"`
}

type ChangesResult struct {
	AccountID      string   `json:"accountId"`
	OldState       string   `json:"oldState"`
	NewState       string   `json:"newState"`
	HasMoreChanges bool     `json:"hasMoreChanges"`
	Created        []string `json:"created"`
	Updated        []string `json:"updated"`
	Destroyed      []string `json:"destroyed"`
}

type SetError struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type EmailSetResult struct {
	AccountID  string                     `json:"accountId"`
	OldState   string                     `json:"oldState"`
	NewState   string                     `json:"newState"`
	Updated    map[string]json.RawMessage `json:"updated"`
	NotUpdated map[string]SetError        `json:"notUpdated"`
}

type MethodCall struct {
	Name      string
	Arguments any
	CallID    string
}

type Request struct {
	Using       []string `json:"using"`
	MethodCalls [][]any  `json:"methodCalls"`
}

func NewRequest(methodCalls ...MethodCall) Request {
	encoded := make([][]any, 0, len(methodCalls))
	for _, call := range methodCalls {
		encoded = append(encoded, []any{call.Name, call.Arguments, call.CallID})
	}
	return Request{
		Using:       []string{CoreCapability, MailCapability},
		MethodCalls: encoded,
	}
}

type Response struct {
	MethodResponses []MethodResponse `json:"-"`
	SessionState    string           `json:"sessionState"`
}

type MethodResponse struct {
	Name   string
	CallID string
	Raw    json.RawMessage
}

type MethodError struct {
	Type        string `json:"type"`
	Status      int    `json:"status"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Detail      string `json:"detail"`
}

func (e MethodError) Error() string {
	switch {
	case e.Description != "":
		return fmt.Sprintf("%s: %s", e.Type, e.Description)
	case e.Detail != "":
		return fmt.Sprintf("%s: %s", e.Type, e.Detail)
	default:
		return e.Type
	}
}

func (r *Response) UnmarshalJSON(data []byte) error {
	var raw struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
		SessionState    string            `json:"sessionState"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.SessionState = raw.SessionState
	r.MethodResponses = make([]MethodResponse, 0, len(raw.MethodResponses))
	for _, item := range raw.MethodResponses {
		var parts []json.RawMessage
		if err := json.Unmarshal(item, &parts); err != nil {
			return fmt.Errorf("decode method response wrapper: %w", err)
		}
		if len(parts) < 3 {
			return fmt.Errorf("malformed method response")
		}

		var name string
		if err := json.Unmarshal(parts[0], &name); err != nil {
			return fmt.Errorf("decode method response name: %w", err)
		}
		var callID string
		if err := json.Unmarshal(parts[2], &callID); err != nil {
			return fmt.Errorf("decode method response call id: %w", err)
		}
		r.MethodResponses = append(r.MethodResponses, MethodResponse{
			Name:   name,
			CallID: callID,
			Raw:    parts[1],
		})
	}
	return nil
}

func Decode[T any](response Response, callID, expectedName string) (T, error) {
	var zero T
	for _, method := range response.MethodResponses {
		if method.CallID != callID {
			continue
		}
		if method.Name == "error" {
			var methodErr MethodError
			if err := json.Unmarshal(method.Raw, &methodErr); err != nil {
				return zero, fmt.Errorf("decode method error: %w", err)
			}
			return zero, methodErr
		}
		if expectedName != "" && method.Name != expectedName {
			return zero, fmt.Errorf("unexpected method response %s for call %s", method.Name, callID)
		}
		var payload T
		if err := json.Unmarshal(method.Raw, &payload); err != nil {
			return zero, fmt.Errorf("decode %s payload: %w", expectedName, err)
		}
		return payload, nil
	}
	return zero, fmt.Errorf("method response for call %s not found", callID)
}

type StateChange struct {
	Type    string                       `json:"@type"`
	Changed map[string]map[string]string `json:"changed"`
}
