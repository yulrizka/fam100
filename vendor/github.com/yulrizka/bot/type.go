package bot

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message represents chat message
type Message struct {
	ID             string
	From           User
	Date           time.Time
	Chat           Chat
	Text           string
	Format         MessageFormat
	ReplyMessageID string
	ReceivedAt     time.Time
	Raw            json.RawMessage `json:"-"`
	Retry          int             `json:"-"`
	DiscardAfter   time.Time       `json:"-"`
}

type ChannelMigratedMessage struct {
	Message
	FromID     string
	ToID       string
	ReceivedAt time.Time
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

// Plugin is pluggable module to process messages
type Plugin interface {
	Name() string
	Init(out chan Message) (chan interface{}, error)
}
