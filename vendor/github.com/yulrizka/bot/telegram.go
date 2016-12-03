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
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
)

// Options
var (
	OutboxBufferSize = 200
	poolDuration     = 1 * time.Second
	log              zap.Logger
	maxMsgPerUpdates = 100
	OutboxWorker     = 5

	// stats
	msgPerUpdateCount   = metrics.NewRegisteredCounter("telegram.messagePerUpdate", metrics.DefaultRegistry)
	updateCount         = metrics.NewRegisteredCounter("telegram.updates.count", metrics.DefaultRegistry)
	updateDuration      = metrics.NewRegisteredTimer("telegram.updates.duration", metrics.DefaultRegistry)
	sendMessageDuration = metrics.NewRegisteredTimer("telegram.sendMessage.duration", metrics.DefaultRegistry)
	msgTimeoutCount     = metrics.NewRegisteredCounter("telegram.sendMessage.timeout", metrics.DefaultRegistry)
	msgFailedCount      = metrics.NewRegisteredCounter("telegram.sendMessage.failed", metrics.DefaultRegistry)
	msgDiscardedCount   = metrics.NewRegisteredCounter("telegram.sendMessage.discarded", metrics.DefaultRegistry)
	msgDroppedCount     = metrics.NewRegisteredCounter("telegram.sendMessage.dropped", metrics.DefaultRegistry)

	// compile time info
	VERSION = ""
)

func init() {
	log = zap.New(zap.NewJSONEncoder(), zap.AddCaller(), zap.AddStacks(zap.FatalLevel))
}

func SetLogger(l zap.Logger) {
	log = l.With(zap.String("module", "bot"))
}

/**
 * Telegram API specific data structure
 */

// TResponse represents response from telegram
type TResponse struct {
	Ok          bool            `json:"ok"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorCode   int64           `json:"error_code,omitempty"`
	Description string          `json:"description"`
}

// TUpdate represents an update event from telegram
type TUpdate struct {
	UpdateID int64           `json:"update_id"`
	Message  json.RawMessage `json:"message"`
}

// TMessage is Telegram incomming message
type TMessage struct {
	MessageID       int64     `json:"message_id"`
	From            TUser     `json:"from"`
	Date            int64     `json:"date"`
	Chat            TChat     `json:"chat"`
	Text            string    `json:"text"`
	ParseMode       string    `json:"parse_mode,omitempty"`
	MigrateToChatID *int64    `json:"migrate_to_chat_id,omitempty"`
	ReplyTo         *TMessage `json:"reply_to_message,omitempty"`
	NewChatMember   TUser     `json:"new_chat_member,omitempty"`
	LeftChatMember  TUser     `json:"left_chat_member,omitempty"`
	ReceivedAt      time.Time `json:"-"`
}

// TOutMessage is Telegram outgoing message
type TOutMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// TUser is Telegram User
type TUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
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
	url        string
	input      map[Plugin]chan interface{}
	output     chan Message
	quit       chan struct{}
	lastUpdate int64
}

// NewTelegram creates telegram API Client
func NewTelegram(key string) *Telegram {
	if key == "" {
		log.Fatal("telegram API key must not be empty")
	}
	return &Telegram{
		url:    fmt.Sprintf("https://api.telegram.org/bot%s", key),
		input:  make(map[Plugin]chan interface{}),
		output: make(chan Message, OutboxBufferSize),
		quit:   make(chan struct{}),
	}
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

					var b bytes.Buffer
					if err := json.NewEncoder(&b).Encode(outMsg); err != nil {
						log.Error("encoding message", zap.Error(err))
						continue
					}
					started := time.Now()
					jsonMsg := b.String()

					var tresp TResponse
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

		var msg interface{}
		message := Message{
			ID: strconv.FormatInt(m.MessageID, 10),
			From: User{
				ID:        strconv.FormatInt(m.From.ID, 10),
				FirstName: m.From.FirstName,
				LastName:  m.From.LastName,
				Username:  m.From.Username,
			},
			Date: time.Unix(m.Date, 0),
			Chat: Chat{
				ID:       strconv.FormatInt(m.Chat.ID, 10),
				Type:     TChatTypeMap[m.Chat.Type],
				Title:    m.Chat.Title,
				Username: m.Chat.Username,
			},
			Text:       m.Text,
			ReceivedAt: receivedAt,
			Raw:        update.Message,
		}
		if m.MigrateToChatID != nil {
			newChanID := strconv.FormatInt(*(m.MigrateToChatID), 10)
			chanMigratedMsg := ChannelMigratedMessage{
				Message:    message,
				FromID:     message.Chat.ID,
				ToID:       newChanID,
				ReceivedAt: receivedAt,
			}
			msg = &chanMigratedMsg
		}
		msg = &message
		log.Debug("update", zap.Object("msg", msg))
		for plugin, ch := range t.input {
			select {
			case ch <- msg:
			default:
				log.Warn("input channel full, skipping message", zap.String("plugin", plugin.Name()), zap.String("msgID", message.ID))
			}
		}
	}

	return len(results), nil
}

func (t *Telegram) Leave(chanID string) error {
	url := fmt.Sprintf("%s/leaveChat?chat_id=%s", t.url, url.QueryEscape(chanID))
	resp, err := http.Get(url)
	if err != nil {
		log.Error("leave failed", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	if _, err := parseResponse(resp); err != nil {
		log.Error("leave invalid response", zap.Error(err))
		return err
	}

	return nil
}

func (t *Telegram) Member(chanID, userID string) (*TChatMember, error) {
	url := fmt.Sprintf("%s/getChatmember?chat_id=%s&user_id=%s", t.url, url.QueryEscape(chanID), url.QueryEscape(userID))
	resp, err := http.Get(url)
	if err != nil {
		log.Error("get member failed", zap.Error(err))
		return nil, err
	}
	defer resp.Body.Close()

	tresp, err := parseResponse(resp)
	if err != nil {
		log.Error("get member invalid response", zap.Error(err))
		return nil, err
	}

	var member TChatMember
	if err := json.Unmarshal(tresp.Result, &member); err != nil {
		return nil, err
	}

	return &member, nil
}

func (t *Telegram) Kick(chanID, userID string) error {
	url := fmt.Sprintf("%s/kickChatMember?chat_id=%s&user_id=%s", t.url, url.QueryEscape(chanID), url.QueryEscape(userID))
	resp, err := http.Get(url)
	if err != nil {
		log.Error("kick failed", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	if _, err := parseResponse(resp); err != nil {
		log.Error("kick invalid response", zap.Error(err))
		return err
	}

	return nil
}

func (t *Telegram) Unban(chanID, userID string) error {
	url := fmt.Sprintf("%s/unbanChatMember?chat_id=%s&user_id=%s", t.url, url.QueryEscape(chanID), url.QueryEscape(userID))
	resp, err := http.Get(url)
	if err != nil {
		log.Error("kick failed", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	if _, err := parseResponse(resp); err != nil {
		log.Error("kick invalid response", zap.Error(err))
		return err
	}

	return nil
}

func parseResponse(resp *http.Response) (TResponse, error) {

	var tresp TResponse
	if err := json.NewDecoder(resp.Body).Decode(&tresp); err != nil {
		return tresp, fmt.Errorf("decoding response failed %s", err)
	}
	if !tresp.Ok {
		return tresp, fmt.Errorf("code:%d description:%s", tresp.ErrorCode, tresp.Description)
	}

	return tresp, nil
}
