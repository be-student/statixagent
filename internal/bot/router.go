// Package bot routes incoming Telegram commands to handlers and formats
// agent state into HTML messages. The router enforces the single-chat
// allowlist (MVP §7): commands from any other chat are dropped.
package bot

import (
	"context"
	"strings"

	"github.com/eliau2005/statixagent/internal/telegram"
)

// Handler answers one command. args holds the words after the command
// ("/ssh history" → ["history"]). The returned string is HTML.
type Handler func(ctx context.Context, args []string) string

// Router dispatches commands for exactly one allowed chat.
type Router struct {
	allowedChat int64
	handlers    map[string]Handler
}

// NewRouter returns a router that only accepts the given chat ID.
func NewRouter(allowedChat int64) *Router {
	return &Router{allowedChat: allowedChat, handlers: map[string]Handler{}}
}

// Handle registers a command (without the leading slash).
func (r *Router) Handle(cmd string, h Handler) {
	r.handlers[cmd] = h
}

// Dispatch processes one update. It returns the reply and true, or
// ("", false) when the update should be ignored (wrong chat, not a
// command). Unknown commands from the allowed chat get a help pointer.
func (r *Router) Dispatch(ctx context.Context, u telegram.Update) (string, bool) {
	if u.ChatID != r.allowedChat || !strings.HasPrefix(u.Text, "/") {
		return "", false
	}
	fields := strings.Fields(u.Text)
	cmd := strings.TrimPrefix(fields[0], "/")
	// "/status@my_bot" form used in groups
	cmd, _, _ = strings.Cut(cmd, "@")
	h, ok := r.handlers[strings.ToLower(cmd)]
	if !ok {
		return "Unknown command. Try /help", true
	}
	return h(ctx, fields[1:]), true
}

// Commands returns the registered command names, for /help.
func (r *Router) Commands() []string {
	out := make([]string, 0, len(r.handlers))
	for c := range r.handlers {
		out = append(out, c)
	}
	return out
}
