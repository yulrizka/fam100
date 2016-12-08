package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
)

// Options
var (
	// OutboxBufferSize is the size of the outbox channel
	OutboxBufferSize = 100
	// OutboxWorker is the number of worker that sends message to telegram api
	OutboxWorker     = 5
	poolDuration     = 1 * time.Second
	log              zap.Logger
	maxMsgPerUpdates = 100

	// stats
	msgPerUpdateCount   = metrics.NewRegisteredCounter("telegram.messagePerUpdate", metrics.DefaultRegistry)
	updateCount         = metrics.NewRegisteredCounter("telegram.updates.count", metrics.DefaultRegistry)
	updateDuration      = metrics.NewRegisteredTimer("telegram.updates.duration", metrics.DefaultRegistry)
	sendMessageDuration = metrics.NewRegisteredTimer("telegram.sendMessage.duration", metrics.DefaultRegistry)
	msgTimeoutCount     = metrics.NewRegisteredCounter("telegram.sendMessage.timeout", metrics.DefaultRegistry)
	msgFailedCount      = metrics.NewRegisteredCounter("telegram.sendMessage.failed", metrics.DefaultRegistry)
	msgDiscardedCount   = metrics.NewRegisteredCounter("telegram.sendMessage.discarded", metrics.DefaultRegistry)
	msgDroppedCount     = metrics.NewRegisteredCounter("telegram.sendMessage.dropped", metrics.DefaultRegistry)

	// VERSION compile time info
	VERSION = ""
)

func init() {
	log = zap.New(zap.NewJSONEncoder(), zap.AddCaller(), zap.AddStacks(zap.FatalLevel))
}

// SetLogger replace the logger object
func SetLogger(l zap.Logger) {
	log = l.With(zap.String("module", "bot"))
}

/**
 * Telegram API specific data structure
 */

// TResponse represents response from telegram
type TResponse struct {
	Ok     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	TError
}

// TError error response structure
type TError struct {
	ErrorCode   int64  `json:"error_code,omitempty"`
	Description string `json:"description"`
}

func (t TError) Error() string {
	return fmt.Sprintf("code:%d description:%q", t.ErrorCode, t.Description)
}

// TUpdate represents an update event from telegram
type TUpdate struct {
	UpdateID int64           `json:"update_id"`
	Message  json.RawMessage `json:"message"`
}

// TMessage is Telegram incomming message
type TMessage struct {
	MessageID       int64           `json:"message_id"`
	From            TUser           `json:"from"`
	Date            int64           `json:"date"`
	Chat            TChat           `json:"chat"`
	Text            string          `json:"text"`
	ParseMode       string          `json:"parse_mode,omitempty"`
	MigrateToChatID *int64          `json:"migrate_to_chat_id,omitempty"`
	ReplyTo         *TMessage       `json:"reply_to_message,omitempty"`
	NewChatMember   *TUser          `json:"new_chat_member,omitempty"`
	LeftChatMember  *TUser          `json:"left_chat_member,omitempty"`
	ReceivedAt      time.Time       `json:"-"`
	Raw             json.RawMessage `json:"-"`
}

// ToMessage converts TMessage to *bot.Message
func (m *TMessage) ToMessage() *Message {
	message := Message{
		ID:   strconv.FormatInt(m.MessageID, 10),
		From: m.From.ToUser(),
		Date: time.Unix(m.Date, 0),
		Chat: Chat{
			ID:       strconv.FormatInt(m.Chat.ID, 10),
			Type:     TChatTypeMap[m.Chat.Type],
			Title:    m.Chat.Title,
			Username: m.Chat.Username,
		},
		Text:       m.Text,
		ReceivedAt: m.ReceivedAt,
		Raw:        m.Raw,
	}

	if m.ReplyTo != nil {
		message.ReplyTo = m.ReplyTo.ToMessage()
	}

	return &message
}

// ToMigratedMessage converts Telegram Message to bot.ChannelMigratedMessage
func (m *TMessage) ToMigratedMessage() ChannelMigratedMessage {
	fromID := strconv.FormatInt(m.Chat.ID, 10)
	toID := strconv.FormatInt(*(m.MigrateToChatID), 10)

	return ChannelMigratedMessage{
		FromID:     fromID,
		ToID:       toID,
		ReceivedAt: m.ReceivedAt,
		Raw:        m.Raw,
	}
}

// TOutMessage is Telegram outgoing message
type TOutMessage struct {
	ChatID           string `json:"chat_id"`
	Text             string `json:"text"`
	ParseMode        string `json:"parse_mode,omitempty"`
	ReplyToMessageID *int64 `json:"reply_to_message_id,omitempty"`
}

// TUser is Telegram User
type TUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// ToUser converts to bot.User
func (u TUser) ToUser() User {
	return User{
		ID:        strconv.FormatInt(u.ID, 10),
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Username:  u.Username,
	}
}

// TChat represents Telegram chat session
type TChat struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	TUser
}

// TChatMember represent user membership of a group
type TChatMember struct {
	User   TUser `json:"user"`
	Status string
}

// TChatTypeMap maps betwwen string to bot.ChatType
var TChatTypeMap = map[string]ChatType{
	"private":    Private,
	"group":      Group,
	"supergroup": SuperGroup,
	"channel":    Channel,
}

// Telegram API
type Telegram struct {
	username   string
	url        string
	input      map[Plugin]chan interface{}
	output     chan Message
	quit       chan struct{}
	lastUpdate int64
}

// NewTelegram creates telegram API Client
func NewTelegram(key string) (*Telegram, error) {
	if key == "" {
		log.Fatal("telegram API key must not be empty")
	}
	t := Telegram{
		url:    fmt.Sprintf("https://api.telegram.org/bot%s", key),
		input:  make(map[Plugin]chan interface{}),
		output: make(chan Message, OutboxBufferSize),
		quit:   make(chan struct{}),
	}

	tresp, err := t.do("getMe")
	if err != nil {
		return nil, errors.Wrap(err, "getMe failed")
	}

	var user TUser
	if err := json.Unmarshal(tresp.Result, &user); err != nil {
		return nil, errors.Wrap(err, "failed decoding response")
	}
	t.username = user.Username

	return &t, nil
}

// Username returns bot's username
func (t *Telegram) Username() string {
	return t.username
}

//AddPlugin add processing module to telegram
func (t *Telegram) AddPlugin(p Plugin) error {
	input, err := p.Init(t.output)
	if err != nil {
		return err
	}
	t.input[p] = input

	return nil
}

// Start consuming from telegram
func (t *Telegram) Start() {
	t.poolOutbox()
	t.poolInbox()
}

func (t *Telegram) poolOutbox() {
	// fork incomming message, group by msg.Chat.ID to the workers
	inChs := make([]chan Message, OutboxWorker)
	for i := 0; i < OutboxWorker; i++ {
		inChs[i] = make(chan Message)
	}

	go func() {
		h := fnv.New32a()
		for {
			h.Reset()
			m := <-t.output
			h.Write([]byte(m.Chat.ID))
			i := int(h.Sum32()) % OutboxWorker
			inChs[i] <- m
		}
	}()

	for i := 0; i < OutboxWorker; i++ {
		go func(i int) {
			input := inChs[i]

		NEXTMESSAGE:
			for {
				select {
				case m := <-input:
					log.Debug("processing message", zap.String("chanID", m.Chat.ID), zap.Int("worker", i))
					if !m.DiscardAfter.IsZero() && time.Now().After(m.DiscardAfter) {
						msgDiscardedCount.Inc(1)
						log.Warn("discarded message", zap.Object("msg", m), zap.Int("worker", i))
						continue
					}

					outMsg := TOutMessage{
						ChatID:    m.Chat.ID,
						Text:      m.Text,
						ParseMode: string(m.Format),
					}
					if m.ReplyToID != "" {
						id, err := strconv.ParseInt(m.ReplyToID, 10, 64)
						if err != nil {
							log.Error("failed to parse ReplyToID", zap.Error(err))
							continue
						}
						outMsg.ReplyToMessageID = &id
					}

					var b bytes.Buffer
					if err := json.NewEncoder(&b).Encode(outMsg); err != nil {
						log.Error("encoding message", zap.Error(err))
						continue
					}
					started := time.Now()
					jsonMsg := b.String()

					var tresp *TResponse
					var err error
					retries := m.Retry
					for {
						if retries < 0 {
							if m.Retry > 0 {
								metrics.GetOrRegisterCounter(fmt.Sprintf("telegram.sendMessage.droppedAfter.%d", m.Retry), metrics.DefaultRegistry).Inc(1)
							}
							log.Error("message dropped, not retrying", zap.Object("msg", m), zap.Int("worker", i))
							msgDroppedCount.Inc(1)
							continue NEXTMESSAGE
						}

						if !m.DiscardAfter.IsZero() && time.Now().After(m.DiscardAfter) {
							log.Error("message dropped, discarded", zap.Object("msg", m), zap.Int("worker", i))
							msgDiscardedCount.Inc(1)
							continue NEXTMESSAGE
						}
						retries--

						var resp *http.Response
						resp, err = http.Post(fmt.Sprintf("%s/sendMessage", t.url), "application/json; charset=utf-10", &b)
						if err != nil {
							msgFailedCount.Inc(1)
							// check for timeout
							if netError, ok := err.(net.Error); ok && netError.Timeout() {
								msgTimeoutCount.Inc(1)
								log.Error("sendMessage timeout", zap.String("ChatID", outMsg.ChatID), zap.Error(err), zap.Int("retries", retries), zap.Int("worker", i))
								continue
							}

							// unknown error
							msgDroppedCount.Inc(1)
							log.Error("sendMessage failed, dropped", zap.String("ChatID", outMsg.ChatID), zap.Error(err), zap.Object("msg", m), zap.Int("worker", i))
							continue NEXTMESSAGE
						}
						metrics.GetOrRegisterCounter(fmt.Sprintf("telegram.sendMessage.http.%d", resp.StatusCode), metrics.DefaultRegistry).Inc(1)

						if resp.StatusCode == 429 { // rate limited by telegram
							msgFailedCount.Inc(1)
							if tresp, err = parseResponse(resp); err != nil {
								log.Error("sendMessage 429", zap.Error(err))
								var delay int
								if n, err := fmt.Sscanf(tresp.Description, "Too Many Requests: retry after %d", &delay); err != nil && n == 1 {
									if delay > 0 {
										d := time.Duration(delay) * time.Second
										log.Warn("sendMessage delayed", zap.String("delay", d.String()))
										time.Sleep(d)
									}
								}
							}
							resp.Body.Close()
							continue
						}

						resp.Body.Close()
						break
					}

					attempt := retries - m.Retry + 1
					metrics.GetOrRegisterCounter(fmt.Sprintf("telegram.sendMessage.retry.%d", attempt), metrics.DefaultRegistry).Inc(1)

					sendMessageDuration.UpdateSince(started)
					if err != nil {
						log.Error("parsing sendMessage response failed", zap.String("ChatID", outMsg.ChatID), zap.Error(err), zap.Object("msg", jsonMsg), zap.Int("worker", i))
					}
				case <-t.quit:
					return
				}
			}
		}(i)
	}
}

func (t *Telegram) poolInbox() {
	for {
		select {
		case <-t.quit:
			return
		default:
			started := time.Now()
			resp, err := http.Get(fmt.Sprintf("%s/getUpdates?offset=%d", t.url, t.lastUpdate+1))
			if err != nil {
				log.Error("getUpdates failed", zap.Error(err))
				updateDuration.UpdateSince(started)
				continue
			}
			updateDuration.UpdateSince(started)
			updateCount.Inc(1)
			metrics.GetOrRegisterCounter(fmt.Sprintf("telegram.getUpdates.http.%d", resp.StatusCode), metrics.DefaultRegistry).Inc(1)

			nMsg, err := t.parseInbox(resp)
			if err != nil {
				log.Error("parsing updates response failed", zap.Error(err))
			}
			msgPerUpdateCount.Inc(int64(nMsg))
			if nMsg != maxMsgPerUpdates {
				time.Sleep(poolDuration)
			}
		}
	}
}

func (t *Telegram) parseInbox(resp *http.Response) (int, error) {
	defer resp.Body.Close()

	receivedAt := time.Now()
	decoder := json.NewDecoder(resp.Body)
	var tresp TResponse
	if err := decoder.Decode(&tresp); err != nil {
		return 0, err
	}

	if !tresp.Ok {
		log.Error("parsing response failed", zap.Int64("errorCode", tresp.ErrorCode), zap.String("description", tresp.Description))
		return 0, nil
	}

	var results []TUpdate
	json.Unmarshal(tresp.Result, &results)
	for _, update := range results {
		var m TMessage
		json.Unmarshal(update.Message, &m)
		t.lastUpdate = update.UpdateID
		m.ReceivedAt = receivedAt
		m.Raw = update.Message

		var msg interface{}
		switch {
		case m.MigrateToChatID != nil:
			msg = m.ToMigratedMessage()
		case m.NewChatMember != nil:
			msg = &JoinMessage{m.ToMessage()}
		case m.LeftChatMember != nil:
			msg = &LeftMessage{m.ToMessage()}
		default:
			msg = m.ToMessage()
		}

		log.Debug("update", zap.Object("msg", msg))
		for plugin, ch := range t.input {
			select {
			case ch <- msg:
			default:
				log.Warn("input channel full, skipping message", zap.String("plugin", plugin.Name()), zap.Int64("msgID", m.MessageID))
			}
		}
	}

	return len(results), nil
}

// Chat gets chat information based on chatID
func (t *Telegram) Chat(id string) (*TChat, error) {
	url := fmt.Sprintf("getChat?chat_id=%s", url.QueryEscape(id))
	resp, err := t.do(url)
	if err != nil {
		return nil, err
	}

	var chat TChat
	if err := json.Unmarshal(resp.Result, &chat); err != nil {
		return nil, err
	}

	return &chat, nil
}

// Leave a chat
func (t *Telegram) Leave(chatID string) error {
	url := fmt.Sprintf("leaveChat?chat_id=%s", url.QueryEscape(chatID))
	_, err := t.do(url)

	return err
}

// Member check if userID is member of chatID
func (t *Telegram) Member(chatID, userID string) (*TChatMember, error) {
	url := fmt.Sprintf("getChatmember?chat_id=%s&user_id=%s", url.QueryEscape(chatID), url.QueryEscape(userID))
	resp, err := t.do(url)
	if err != nil {
		return nil, err
	}

	var member TChatMember
	if err := json.Unmarshal(resp.Result, &member); err != nil {
		return nil, err
	}

	return &member, nil
}

// MembersCount gets the counts of member for a chat id
func (t *Telegram) MembersCount(chatID string) (int, error) {
	url := fmt.Sprintf("getChatMembersCount?chat_id=%s", url.QueryEscape(chatID))
	resp, err := t.do(url)
	if err != nil {
		return 0, err
	}

	var n int
	err = json.Unmarshal(resp.Result, &n)

	return n, err
}

// Kick userID from chatID
func (t *Telegram) Kick(chatID, userID string) error {
	url := fmt.Sprintf("kickChatMember?chat_id=%s&user_id=%s", url.QueryEscape(chatID), url.QueryEscape(userID))
	_, err := t.do(url)
	return err
}

// Unban userID from chatID
func (t *Telegram) Unban(chatID, userID string) error {
	url := fmt.Sprintf("unbanChatMember?chat_id=%s&user_id=%s", url.QueryEscape(chatID), url.QueryEscape(userID))
	_, err := t.do(url)
	return err
}

func (t *Telegram) do(urlPath string) (*TResponse, error) {
	url := fmt.Sprintf("%s/%s", t.url, urlPath)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return parseResponse(resp)
}

func parseResponse(resp *http.Response) (*TResponse, error) {
	var tresp TResponse
	if err := json.NewDecoder(resp.Body).Decode(&tresp); err != nil {
		return nil, fmt.Errorf("decoding response failed %s", err)
	}
	if !tresp.Ok {
		return nil, tresp.TError
	}

	return &tresp, nil
}

//TelegramEscape escapes html that is acceptable by telegram
func TelegramEscape(s string) string {
	s = strings.Replace(s, "&", "&amp;", -1)
	s = strings.Replace(s, "<", "&lt;", -1)
	s = strings.Replace(s, ">", "&gt;", -1)

	return s
}
