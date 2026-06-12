// Package telegram is a minimal Telegram Bot API client: sendMessage and a
// getUpdates long-poll. No SDK — the agent uses exactly two endpoints and a
// hand-rolled client keeps the binary small (MVP §1).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Update is one incoming message, reduced to what the bot needs.
type Update struct {
	ID     int64 // update_id, used as the getUpdates offset cursor
	ChatID int64
	Text   string
}

// Client talks to the Bot API for one bot token.
type Client struct {
	HTTP *http.Client
	// BaseURL includes the token: https://api.telegram.org/bot<token>
	BaseURL string
}

// New returns a client for the given bot token.
func New(token string) *Client {
	return &Client{
		// Long-poll timeout is 50s server-side; the client must outlive it.
		HTTP:    &http.Client{Timeout: 70 * time.Second},
		BaseURL: "https://api.telegram.org/bot" + token,
	}
}

// apiResponse is the Bot API envelope.
type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (c *Client) call(ctx context.Context, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()
	var env apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	if !env.OK {
		return fmt.Errorf("telegram: %s: %s", method, env.Description)
	}
	if result != nil {
		return json.Unmarshal(env.Result, result)
	}
	return nil
}

// SendMessage sends an HTML-formatted message to a chat. Messages longer
// than Telegram's 4096-char limit are split on line boundaries.
func (c *Client) SendMessage(ctx context.Context, chatID int64, html string) error {
	for _, chunk := range splitMessage(html, 4096) {
		payload := map[string]any{
			"chat_id":                  chatID,
			"text":                     chunk,
			"parse_mode":               "HTML",
			"disable_web_page_preview": true,
		}
		if err := c.call(ctx, "sendMessage", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

// GetUpdates long-polls for new messages after offset. It returns plain
// text messages only; the caller filters by chat ID.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	payload := map[string]any{
		"offset":          offset,
		"timeout":         int(timeout.Seconds()),
		"allowed_updates": []string{"message"},
	}
	var raw []struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			Text string `json:"text"`
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	}
	if err := c.call(ctx, "getUpdates", payload, &raw); err != nil {
		return nil, err
	}
	out := make([]Update, 0, len(raw))
	for _, r := range raw {
		u := Update{ID: r.UpdateID}
		if r.Message != nil {
			u.ChatID = r.Message.Chat.ID
			u.Text = r.Message.Text
		}
		out = append(out, u)
	}
	return out, nil
}

// splitMessage breaks text into chunks of at most limit characters,
// preferring newline boundaries.
func splitMessage(s string, limit int) []string {
	if len(s) <= limit {
		return []string{s}
	}
	var chunks []string
	for len(s) > limit {
		cut := limit
		if i := lastIndexByteBefore(s, '\n', limit); i > 0 {
			cut = i
		}
		chunks = append(chunks, s[:cut])
		s = s[cut:]
		if len(s) > 0 && s[0] == '\n' {
			s = s[1:]
		}
	}
	if s != "" {
		chunks = append(chunks, s)
	}
	return chunks
}

func lastIndexByteBefore(s string, b byte, before int) int {
	for i := before - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
