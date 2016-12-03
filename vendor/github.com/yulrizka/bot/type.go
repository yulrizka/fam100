package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Client represent a chat client. Currently supports telegram
type Client interface {
	AddPlugin(Plugin) error
	Start()
	Username() string // bot username
}

// Message represents chat message
type Message struct {
	ID           string
	From         User
	Date         time.Time
	Chat         Chat
	Text         string
	Format       MessageFormat
	ReplyTo      *Message
	ReplyToID    string
	ReceivedAt   time.Time
	Raw          json.RawMessage `json:"-"`
	Retry        int             `json:"-"`
	DiscardAfter time.Time       `json:"-"`
}

// JoinMessage represents information that a user join a chat
type JoinMessage struct {
	*Message
}

// LeftMessage represents information that a user left a chat
type LeftMessage struct {
	*Message
}

// ChannelMigratedMessage represents that a chat type has been upgraded. Currently works on telegram
type ChannelMigratedMessage struct {
	FromID     string
	ToID       string
	ReceivedAt time.Time
	Raw        json.RawMessage `json:"-"`
}

// MessageFormat represents formatting of the message
type MessageFormat string

// Available MessageFormat
const (
	Text     MessageFormat = ""
	Markdown MessageFormat = "markdown"
	HTML     MessageFormat = "html"
)

// User represents user information
type User struct {
	ID        string
	FirstName string
	LastName  string
	Username  string
}

// FullName returns Firstname + LastName
func (u User) FullName() string {
	if u.LastName != "" {
		return fmt.Sprintf("%s %s", u.FirstName, u.LastName)
	}
	return u.FirstName
}

// ChatType is type of the message
type ChatType string

// Available ChatType
const (
	Private    ChatType = "private"
	Group      ChatType = "group"
	SuperGroup ChatType = "supergroup"
	Channel    ChatType = "channel"
)

// Chat represents a chat session
type Chat struct {
	ID       string
	Type     ChatType
	Title    string
	Username string
}

// Name returns the title of the chat
func (t Chat) Name() string {
	var buf bytes.Buffer
	buf.WriteString(t.Title)
	if t.Username != "" {
		buf.WriteString(" (@")
		buf.WriteString(t.Username)
		buf.WriteString(")")
	}

	return buf.String()
}

// Plugin is pluggable module to process messages
type Plugin interface {
	Name() string
	Init(out chan Message) (chan interface{}, error)
}
