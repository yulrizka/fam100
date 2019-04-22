package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Client represent a chat client. Currently supports telegram
type Client interface {
	// AddPlugins will add each plugin as middleware, each message will flow to the Handle for each plugin unless
	// previous plugin return handled equals true
	AddPlugins(...Plugin) error
	// Start listening for new messages and block
	Start(context.Context) error
	// UserName of the bot
	UserName() string
	//Mentioned will return true filed is a mention to a user
	Mentioned(field string) bool
	// Mention a user
	Mention(user User) string
	// UserByName find user by username
	UserByName(username string) (User, bool)
	// Get chat information such as topic etc
	ChatInfo(ctx context.Context, chatID string) (ChatInfo, error)
	// SetTopic for a channel
	SetTopic(ctx context.Context, chatID, topic string) error
	//UploadFile to a channel
	UploadFile(ctx context.Context, chatID string, filename string, r io.Reader) error
}

type Attachment struct {
	Fallback string `json:"fallback"`
	Text     string `json:"text"`
	Pretext  string `json:"pretext"`
	Title    string `json:"title"`
	ID       int64  `json:"id"`
}

// Message represents chat message
type Message struct {
	ctx          context.Context
	ID           string
	From         User
	Date         time.Time
	Chat         Chat
	Text         string
	Format       MessageFormat
	ReplyTo      *Message
	ReplyToID    string
	ReceivedAt   time.Time
	Attachments  []Attachment
	Raw          json.RawMessage `json:"-"`
	Retry        int             `json:"-"`
	DiscardAfter time.Time       `json:"-"`
}

func (m *Message) Context() context.Context {
	return m.ctx
}

func (m *Message) WithContext(ctx context.Context) *Message {
	if ctx == nil {
		panic("nil context")
	}
	m2 := new(Message)
	m2 = m
	m2.ctx = ctx
	return m2
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

// FullName returns first name + last name
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
	Channel    ChatType = "channel"
	Group      ChatType = "group"
	Private    ChatType = "private"
	SuperGroup ChatType = "supergroup"
	Thread     ChatType = "thread"
)

// Chat represents a chat session
type Chat struct {
	ID       string
	Type     ChatType
	Title    string
	Username string
}

type ChatInfo struct {
	ID          string
	Type        ChatType
	Title       string
	Topic       string
	Description string
}

// Name returns the title of the bot
func (t Chat) Name() string {
	return fmt.Sprintf(" (@%s)", t.Username)
}

// Plugin is pluggable module to process messages
type Plugin interface {
	Name() string
	Init(ctx context.Context, out chan Message, cl Client) error
	Handle(ctx context.Context, in interface{}) (handled bool, msg interface{})
}
