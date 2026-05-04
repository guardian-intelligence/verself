package jmap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Event struct {
	ID   string
	Name string
	Data []byte
}

type EventStream struct {
	resp   *http.Response
	reader *bufio.Reader
}

func (c *Client) OpenEventSource(ctx context.Context, types []string, ping time.Duration) (*EventStream, error) {
	values := url.Values{}
	values.Set("types", strings.Join(types, ","))
	values.Set("closeafter", "no")
	values.Set("ping", fmt.Sprintf("%d", int(ping/time.Second)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jmap/eventsource/?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build eventsource request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open eventsource: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("open eventsource: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return &EventStream{
		resp:   resp,
		reader: bufio.NewReader(resp.Body),
	}, nil
}

func (s *EventStream) Close() error {
	if s == nil || s.resp == nil || s.resp.Body == nil {
		return nil
	}
	return s.resp.Body.Close()
}

func (s *EventStream) Next() (Event, error) {
	var event Event
	var dataLines []string

	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && (event.Name != "" || len(dataLines) > 0 || event.ID != "") {
				event.Data = []byte(strings.Join(dataLines, "\n"))
				return event, nil
			}
			return Event{}, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Name == "" && len(dataLines) == 0 && event.ID == "" {
				continue
			}
			event.Data = []byte(strings.Join(dataLines, "\n"))
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field := line
		value := ""
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			field = line[:idx]
			value = strings.TrimPrefix(line[idx+1:], " ")
		}

		switch field {
		case "event":
			event.Name = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			event.ID = value
		}
	}
}

func (e Event) DecodeStateChange() (StateChange, error) {
	var change StateChange
	if err := json.Unmarshal(e.Data, &change); err != nil {
		return StateChange{}, err
	}
	return change, nil
}
