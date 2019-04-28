package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rcrowley/go-metrics"
)

// Options
var (
	// OutboxBufferSize is the size of the outbox channel
	OutboxBufferSize = 100
	// OutboxWorker is the number of worker that sends message to telegram api
	OutboxWorker     = 5
	poolDuration     = 1 * time.Second
	maxMsgPerUpdates = 100

	// VERSION compile time info
	VERSION = ""
)

// Metrics for telegram
var (
	StatsMsgPerUpdateCount   = metrics.NewRegisteredCounter("telegram.messagePerUpdate", metrics.DefaultRegistry)
	StatsUpdateCount         = metrics.NewRegisteredCounter("telegram.updates.count", metrics.DefaultRegistry)
	StatsUpdateDuration      = metrics.NewRegisteredTimer("telegram.updates.duration", metrics.DefaultRegistry)
	StatsSendMessageDuration = metrics.NewRegisteredTimer("telegram.sendMessage.duration", metrics.DefaultRegistry)
	StatsMsgTimeoutCount     = metrics.NewRegisteredCounter("telegram.sendMessage.timeout", metrics.DefaultRegistry)
	StatsMsgFailedCount      = metrics.NewRegisteredCounter("telegram.sendMessage.failed", metrics.DefaultRegistry)
	StatsMsgDiscardedCount   = metrics.NewRegisteredCounter("telegram.sendMessage.discarded", metrics.DefaultRegistry)
	StatsMsgDroppedCount     = metrics.NewRegisteredCounter("telegram.sendMessage.dropped", metrics.DefaultRegistry)
)

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
	ctx      context.Context
	quit     context.CancelFunc
	username string
	url      string
	output   chan Message

	lastUpdate int64
	plugins    []Plugin
	handler    func(interface{}) (handled bool, msg interface{})
}

// NewTelegram creates telegram API Client
//noinspection GoUnusedExportedFunction
func NewTelegram(ctx context.Context, key string) (*Telegram, error) {
	if key == "" {
		return nil, errors.New("empty key")
	}
	ctx, quit := context.WithCancel(ctx)
	t := Telegram{
		ctx:    ctx,
		quit:   quit,
		url:    fmt.Sprintf("https://api.telegram.org/bot%s", key),
		output: make(chan Message, OutboxBufferSize),
		handler: func(inMsg interface{}) (handled bool, msg interface{}) {
			return true, inMsg
		},
	}

	tresp, err := t.do(ctx, "getMe")
	if err != nil {
		return nil, fmt.Errorf("getMe failed: %v", err)
	}

	var user TUser
	if err := json.Unmarshal(tresp.Result, &user); err != nil {
		return nil, fmt.Errorf("failed decoding response: %v", err)
	}
	t.username = user.Username

	return &t, nil
}

// Username returns bot's username
func (t *Telegram) UserName() string {
	return t.username
}

//AddPlugin add processing module to telegram
func (t *Telegram) AddPlugins(plugins ...Plugin) error {
	if len(plugins) == 0 {
		return nil
	}
	for i := len(plugins) - 1; i >= 0; i-- {
		p := plugins[i]
		t.plugins = append(t.plugins, p)
	}

	return nil
}

func (t *Telegram) initPlugins(ctx context.Context) error {
	for _, p := range t.plugins {
		err := p.Init(ctx, t.output, t)
		if err != nil {
			return err
		}

		// add middle ware
		next := t.handler
		t.handler = func(inMsg interface{}) (handled bool, msg interface{}) {
			ok, msg := p.Handle(ctx, inMsg)
			if ok {
				return true, msg
			}
			return next(msg)
		}
		log(Info, fmt.Sprintf("Added plugin name:%s", p.Name()))
	}

	return nil
}

// Start consuming from telegram
func (t *Telegram) Start(ctx context.Context) error {
	err := t.initPlugins(ctx)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %v", err)
	}

	t.poolOutbox()
	t.poolInbox()
	<-t.ctx.Done()

	return nil
}

func (t *Telegram) Stop() {
}

func (t *Telegram) poolOutbox() {
	// fork incoming message, group by msg.Chat.ID to the workers
	inChs := make([]chan Message, OutboxWorker)
	for i := 0; i < OutboxWorker; i++ {
		inChs[i] = make(chan Message)
	}

	go func() {
		h := fnv.New32a()
		for {
			h.Reset()
			m := <-t.output
			_, err := h.Write([]byte(m.Chat.ID))
			if err != nil {
				log(Error, fmt.Sprintf("failed when writing hash: %v", err))
			}

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
					log(Debug, fmt.Sprintf("processing message chanID:%s worker:%d", m.Chat.ID, i))

					if !m.DiscardAfter.IsZero() && time.Now().After(m.DiscardAfter) {
						StatsMsgDiscardedCount.Inc(1)
						log(Warn, fmt.Sprintf("discarded message msg:%+v worker:%d", m, i))
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
							log(Error, fmt.Sprintf("failed to parse ReplyToID: %v", err))
							continue
						}
						outMsg.ReplyToMessageID = &id
					}

					var b bytes.Buffer
					if err := json.NewEncoder(&b).Encode(outMsg); err != nil {
						log(Error, fmt.Sprintf("failed to encoding message: %v", err))
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
							log(Error, fmt.Sprintf("message dropped, not retrying msg:%+v worker:%d", m, i))
							StatsMsgDroppedCount.Inc(1)
							continue NEXTMESSAGE
						}

						if !m.DiscardAfter.IsZero() && time.Now().After(m.DiscardAfter) {
							log(Error, fmt.Sprintf("message dropped msg:%+v worker:%d", m, i))
							StatsMsgDiscardedCount.Inc(1)
							continue NEXTMESSAGE
						}
						retries--

						var resp *http.Response
						resp, err = http.Post(fmt.Sprintf("%s/sendMessage", t.url), "application/json; charset=utf-10", &b)
						if err != nil {
							StatsMsgFailedCount.Inc(1)
							// check for timeout
							if netError, ok := err.(net.Error); ok && netError.Timeout() {
								StatsMsgTimeoutCount.Inc(1)
								log(Warn, fmt.Sprintf("timeout sending message ChatID:%s retries:%d worker:%d : %v", outMsg.ChatID, retries, i, err))
								continue
							}

							// unknown error
							StatsMsgDroppedCount.Inc(1)

							log(Error, fmt.Sprintf("sendMessage failed, dropped ChatID:%s retries:%d worker:%d : %v", outMsg.ChatID, retries, i, err))
							continue NEXTMESSAGE
						}
						metrics.GetOrRegisterCounter(fmt.Sprintf("telegram.sendMessage.http.%d", resp.StatusCode), metrics.DefaultRegistry).Inc(1)

						if resp.StatusCode == 429 { // rate limited by telegram
							StatsMsgFailedCount.Inc(1)
							if tresp, err = parseResponse(resp); err != nil {
								var delay int
								if n, err := fmt.Sscanf(tresp.Description, "Too Many Requests: retry after %d", &delay); err != nil && n == 1 {
									if delay > 0 {
										log(Warn, fmt.Sprintf("failed sending message, rate limited, will retry after %d second ChatID:%s retries:%d worker:%d: %v",
											delay, outMsg.ChatID, retries, i, err))
										d := time.Duration(delay) * time.Second
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

					StatsSendMessageDuration.UpdateSince(started)
					if err != nil {
						log(Error, fmt.Sprintf("send message failed %s, msg:%+v worker:%d : failed parsing response : %v", outMsg.ChatID, jsonMsg, i, err))
					}
				case <-t.ctx.Done():
					return
				}
			}
		}(i)
	}
}

func (t *Telegram) poolInbox() {
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			started := time.Now()
			resp, err := http.Get(fmt.Sprintf("%s/getUpdates?offset=%d", t.url, t.lastUpdate+1))

			if err != nil {
				log(Error, fmt.Sprintf("get new message failed: %v", err))
				StatsUpdateDuration.UpdateSince(started)
				continue
			}
			StatsUpdateDuration.UpdateSince(started)
			StatsUpdateCount.Inc(1)
			metrics.GetOrRegisterCounter(fmt.Sprintf("telegram.getUpdates.http.%d", resp.StatusCode), metrics.DefaultRegistry).Inc(1)

			nMsg, err := t.parseInbox(resp)
			if err != nil {
				log(Error, fmt.Sprintf("parsing new response message failed: %v", err))
			}
			StatsMsgPerUpdateCount.Inc(int64(nMsg))
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
		log(Error, fmt.Sprintf("parsing response failed errorCode:%d description:%s", tresp.ErrorCode, tresp.Description))
		return 0, nil
	}

	var results []TUpdate
	err := json.Unmarshal(tresp.Result, &results)
	if err != nil {
		return 0, fmt.Errorf("json marshall failed: %v", err)
	}

	for _, update := range results {
		var m TMessage
		err := json.Unmarshal(update.Message, &m)
		if err != nil {
			log(Error, fmt.Sprintf("json marshall failed, continue with the next result: %v", err))
			continue
		}

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

		log(Debug, fmt.Sprintf("new message: %+v", msg))
		t.handler(msg)

	}

	return len(results), nil
}

func (t *Telegram) ChatInfo(ctx context.Context, chatID string) (ChatInfo, error) {
	ci := ChatInfo{}
	tchat, err := t.Chat(ctx, chatID)
	if err != nil {
		return ci, err
	}

	ci.ID = strconv.FormatInt(tchat.ID, 10)
	ci.Type = Group
	ci.Title = tchat.Title
	return ci, nil
}

// Chat gets chat information based on chatID
func (t *Telegram) Chat(ctx context.Context, id string) (*TChat, error) {
	s := fmt.Sprintf("getChat?chat_id=%s", url.QueryEscape(id))
	resp, err := t.do(ctx, s)
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
func (t *Telegram) Leave(ctx context.Context, chatID string) error {
	s := fmt.Sprintf("leaveChat?chat_id=%s", url.QueryEscape(chatID))
	_, err := t.do(ctx, s)

	return err
}

// Member check if userID is member of chatID
func (t *Telegram) Member(ctx context.Context, chatID, userID string) (*TChatMember, error) {
	s := fmt.Sprintf("getChatmember?chat_id=%s&user_id=%s", url.QueryEscape(chatID), url.QueryEscape(userID))
	resp, err := t.do(ctx, s)
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
func (t *Telegram) MembersCount(ctx context.Context, chatID string) (int, error) {
	s := fmt.Sprintf("getChatMembersCount?chat_id=%s", url.QueryEscape(chatID))
	resp, err := t.do(ctx, s)
	if err != nil {
		return 0, err
	}

	var n int
	err = json.Unmarshal(resp.Result, &n)

	return n, err
}

// Kick userID from chatID
func (t *Telegram) Kick(ctx context.Context, chatID, userID string) error {
	s := fmt.Sprintf("kickChatMember?chat_id=%s&user_id=%s", url.QueryEscape(chatID), url.QueryEscape(userID))
	_, err := t.do(ctx, s)
	return err
}

// Unban userID from chatID
func (t *Telegram) Unban(ctx context.Context, chatID, userID string) error {
	s := fmt.Sprintf("unbanChatMember?chat_id=%s&user_id=%s", url.QueryEscape(chatID), url.QueryEscape(userID))
	_, err := t.do(ctx, s)
	return err
}

func (t *Telegram) SetTopic(ctx context.Context, chatID string, topic string) error {
	return fmt.Errorf("telegram: Set topic is not implemented")
}

func (t *Telegram) do(ctx context.Context, urlPath string) (*TResponse, error) {
	s := fmt.Sprintf("%s/%s", t.url, urlPath)
	req, err := http.NewRequest("GET", s, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return parseResponse(resp)
}

func (t *Telegram) Mentioned(field string) bool {
	panic("TODO not implemented")
}

func (t *Telegram) Mention(u User) string {
	return "@" + u.Username
}

func (t *Telegram) UserByName(username string) (User, bool) {
	panic("TODO not implemented")
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

func (t *Telegram) UploadFile(ctx context.Context, chatID string, filename string, r io.Reader) error {
	panic("TODO not implemented")
}

//TelegramEscape escapes html that is acceptable by telegram
//noinspection GoUnusedExportedFunction
func TelegramEscape(s string) string {
	s = strings.Replace(s, "&", "&amp;", -1)
	s = strings.Replace(s, "<", "&lt;", -1)
	s = strings.Replace(s, ">", "&gt;", -1)

	return s
}
