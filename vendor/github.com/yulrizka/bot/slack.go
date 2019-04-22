package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const slackURL = "https://slack.com/api"

var (
	ignoreMessageType = map[string]bool{
		"channel_joined":  true,
		"channel_created": true,
		"channel_rename":  true,
		"im_created":      true,
		"team_join":       true,
		"user_change":     true,
	}
)

type Slack struct {
	token  string
	input  map[Plugin]chan interface{}
	output chan Message

	handler func(interface{}) (handled bool, msg interface{})
	plugins []Plugin

	url            string
	teamID         string
	teamName       string
	domain         string
	enterpriseID   string
	enterpriseName string
	id             string
	name           string

	idToMember       map[string]slackUser
	userNameToMember map[string]slackUser
	ims              map[string]slackIm
	channels         map[string]slackChannel
	nameToChannels   map[string]slackChannel
}

type slackUser struct {
	ID                 string
	TeamID             string
	Name               string
	Deleted            bool
	Color              string
	RealName           string
	RealNameNormalized string
	TZ                 string
	TZLabel            string
	TZOffset           int64
	Profile            struct {
		FirstName      string
		LastName       string
		AvatarHash     string
		Image24        string
		Image32        string
		Image48        string
		Image72        string
		Image192       string
		Image512       string
		Image1024      string
		ImageOriginal  string
		Title          string
		Phone          string
		GuestChannels  string
		GuestInvitedBy string
		Email          string
	}
	IsAdmin           bool
	IsOwner           bool
	IsPrimaryOwner    bool
	IsRestricted      bool
	IsUltraRestricted bool
	IsBot             bool
	Updated           int64
	EnterpriseUser    struct {
		ID             string
		EnterpriseID   string
		EnterpriseName string
		IsAdmin        bool
		IsOwner        bool
		Teams          []string
	}
}

type slackIm struct {
	ID            string
	IsIM          bool
	User          string
	Created       int64
	IsUserDeleted bool
}

type slackChannel struct {
	Id         string
	Name       string
	Created    int64
	Creator    string
	IsArchived bool
	IsMember   bool
	NumMembers int64
	Topic      struct {
		Value   string
		Creator string
		LastSet int64
	}
	Purpose struct {
		Value   string
		Creator string
		LastSet int64
	}
}

type slackResponse struct {
	Ok      bool
	Error   string
	Warning string
}

type slackError struct {
	Message    string
	ErrorMsg   string
	WarningMsg string
}

type slackConversation struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	IsChannel          bool          `json:"is_channel"`
	IsGroup            bool          `json:"is_group"`
	IsIm               bool          `json:"is_im"`
	Created            int           `json:"created"`
	Creator            string        `json:"creator"`
	IsArchived         bool          `json:"is_archived"`
	IsGeneral          bool          `json:"is_general"`
	Unlinked           int           `json:"unlinked"`
	NameNormalized     string        `json:"name_normalized"`
	IsReadOnly         bool          `json:"is_read_only"`
	IsShared           bool          `json:"is_shared"`
	ParentConversation interface{}   `json:"parent_conversation"`
	IsExtShared        bool          `json:"is_ext_shared"`
	IsOrgShared        bool          `json:"is_org_shared"`
	PendingShared      []interface{} `json:"pending_shared"`
	IsPendingExtShared bool          `json:"is_pending_ext_shared"`
	IsMember           bool          `json:"is_member"`
	IsPrivate          bool          `json:"is_private"`
	IsMpim             bool          `json:"is_mpim"`
	LastRead           string        `json:"last_read"`
	Topic              struct {
		Value   string `json:"value"`
		Creator string `json:"creator"`
		LastSet int    `json:"last_set"`
	} `json:"topic"`
	Purpose struct {
		Value   string `json:"value"`
		Creator string `json:"creator"`
		LastSet int    `json:"last_set"`
	} `json:"purpose"`
	PreviousNames []string `json:"previous_names"`
	Locale        string   `json:"locale"`
}

func (ew slackError) Error() string {
	return fmt.Sprintf("%s error:%q warning:%q", ew.Message, ew.ErrorMsg, ew.WarningMsg)
}

func NewSlack(token string) (*Slack, error) {
	return &Slack{
		token:  token,
		input:  make(map[Plugin]chan interface{}),
		output: make(chan Message, OutboxBufferSize),
		handler: func(inMsg interface{}) (handled bool, msg interface{}) {
			return true, inMsg
		},
	}, nil
}

func (s *Slack) AddPlugins(plugins ...Plugin) error {
	if len(plugins) == 0 {
		return nil
	}
	for i := len(plugins) - 1; i >= 0; i-- {
		p := plugins[i]
		s.plugins = append(s.plugins, p)
	}

	return nil
}

func (s *Slack) Start(ctx context.Context) error {
	if err := s.init(ctx); err != nil {
		return fmt.Errorf("failed to initialize connection: %s", err)
	}

	s.initPlugins(ctx)

	conn, _, err := websocket.DefaultDialer.Dial(s.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket %q: %s", s.url, err)
	}

	// handle incoming message
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, raw, err := conn.ReadMessage()
				if err != nil {
					log(Error, fmt.Sprintf("failed to receive data: %v", err))
					continue
				}

				msg, err := s.parseIncomingMessage(raw)
				if err != nil {
					log(Error, fmt.Sprintf("failed to parse message raw:%s : %v", string(raw), err))
					continue
				}
				if msg == nil {
					continue
				}
				// ignore message from our self
				if msg.From.ID == s.id || msg.From.Username == s.name {
					continue
				}
				s.handler(msg)
			}
		}
	}()

	// handle outgoing message
	go func() {
		var counter int64
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-s.output:
				counter++
				outMsg := struct {
					ID       int64  `json:"id"`
					Type     string `json:"type"`
					Channel  string `json:"channel"`
					Text     string `json:"text"`
					ThreadTs string `json:"thread_ts,omitempty"`
					Mrkdwn   bool   `json:"mrkdwn"`
				}{counter, "message", msg.Chat.ID, msg.Text, "", msg.Format == Markdown}

				switch msg.Chat.Type {
				case Private:
					if strings.HasPrefix(msg.Chat.ID, "D") {
						// possibly already a valid chat id
						break
					}
					var idOrUsername = msg.Chat.ID
					if idOrUsername == "" {
						idOrUsername = msg.Chat.Username
					}
					channel, err := s.imID(ctx, idOrUsername)
					if err != nil {
						log(Error, fmt.Sprintf("failed to get chat %+v: %v", outMsg, err))
						continue
					}
					msg.Chat.ID = channel
					outMsg.Channel = channel
				case Thread:
					outMsg.ThreadTs = msg.ReplyTo.ID
				}

				// if message has attachment, we must use the web API
				if len(msg.Attachments) > 0 {
					if err := s.chatPostMessage(ctx, msg); err != nil {
						log(Error, fmt.Sprintf("failed to send message msg:%+v: %v", outMsg, err))
					}
					continue
				}

				if err := conn.WriteJSON(&outMsg); err != nil {
					log(Error, fmt.Sprintf("failed to send message msg:%+v: %v", outMsg, err))
					continue
				}
			case <-t.C:
				counter++
				ping := struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				}{counter, "ping"}
				if err := conn.WriteJSON(ping); err != nil {
					log(Warn, fmt.Sprintf("failed to send ping: %v", err))
				}
			}
		}

	}()
	<-ctx.Done()
	return nil
}

func (s *Slack) initPlugins(ctx context.Context) {
	for _, plugin := range s.plugins {
		p := plugin
		if err := p.Init(ctx, s.output, s); err != nil {
			log(Error, fmt.Sprintf("failed to initialize, plugin %q will be disabled: %v", p.Name(), err))
			continue
		}

		// add middle ware
		next := s.handler
		s.handler = func(inMsg interface{}) (handled bool, msg interface{}) {
			ok, msg := p.Handle(ctx, inMsg)
			if ok {
				return true, msg
			}
			return next(msg)
		}
		log(Info, fmt.Sprintf("Initialized %q", p.Name()))
	}
}

func (s *Slack) init(ctx context.Context) error {
	log(Info, fmt.Sprintf("Initializing Slack"))
	data := url.Values{}
	data.Set("token", s.token)

	resp, err := s.doPost(ctx, slackURL+"/rtm.connect", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("faile to create connect request: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
		URL  string
		Team struct {
			ID             string
			Name           string
			Domain         string
			EnterpriseID   string
			EnterpriseName string
		}
		Self struct {
			ID   string
			Name string
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return fmt.Errorf("failed to parse response: %s", err)

	}
	if !sResp.Ok {
		return fmt.Errorf("failed to connect error:%s warning:%s", sResp.Error, sResp.Warning)
	}
	s.url = sResp.URL
	s.teamID = sResp.Team.ID
	s.teamName = sResp.Team.Name
	s.domain = sResp.Team.Domain
	s.enterpriseID = sResp.Team.EnterpriseID
	s.enterpriseName = sResp.Team.EnterpriseName
	s.id = sResp.Self.ID
	s.name = sResp.Self.Name

	errCh := make(chan error)
	go func() {
		var err error
		defer func() {
			errCh <- err
		}()

		var members map[string]slackUser
		members, err = s.userList(ctx)
		if err != nil {
			err = fmt.Errorf("failed to get list of user: %s", err)
			return
		}
		s.idToMember = members

		s.userNameToMember = make(map[string]slackUser)
		for _, user := range members {
			s.userNameToMember[user.Name] = user
		}
	}()

	go func() {
		var err error
		defer func() {
			errCh <- err
		}()

		var ims map[string]slackIm
		ims, err = s.imList(ctx)
		if err != nil {
			err = fmt.Errorf("failed to get list of user: %s", err)
			return
		}
		s.ims = ims
	}()

	errs := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	close(errCh)
	if len(errs) > 0 {
		return fmt.Errorf("init failuers: %s", strings.Join(errs, ";"))
	}

	log(Info, fmt.Sprintf("initialize completed botname: %s id:%s", s.name, s.id))

	// since it's not used and quite some big response, skip it for now
	enableFetchChannels := true
	go func() {
		if !enableFetchChannels {
			return
		}

		var channels map[string]slackChannel
		channels, err = s.channelsList(ctx)
		if err != nil {
			log(Error, fmt.Sprintf("failed to get list of channels: +%v", err))
			return
		}
		s.channels = channels
		s.nameToChannels = make(map[string]slackChannel)
		for _, ch := range channels {
			s.nameToChannels[ch.Name] = ch
		}
	}()

	return nil
}

func (s *Slack) userList(ctx context.Context) (map[string]slackUser, error) {
	data := url.Values{}
	data.Set("token", s.token)
	data.Set("presence", "true")

	resp, err := s.doPost(ctx, slackURL+"/users.list", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("faile to create connect request: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
		Members []slackUser
	}
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %s", err)

	}
	if !sResp.Ok {
		return nil, fmt.Errorf("userList failed error:%s warning:%s", sResp.Error, sResp.Warning)
	}

	members := make(map[string]slackUser)
	for _, member := range sResp.Members {
		members[member.ID] = member
	}
	return members, nil
}

func (s *Slack) imList(ctx context.Context) (map[string]slackIm, error) {
	data := url.Values{}
	data.Set("token", s.token)

	resp, err := s.doPost(ctx, slackURL+"/im.list", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("faile to create connect request: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
		Ims []slackIm
	}

	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %s", err)

	}
	if !sResp.Ok {
		return nil, fmt.Errorf("failed to connect error:%s warning:%s", sResp.Error, sResp.Warning)
	}

	ims := make(map[string]slackIm)
	for _, im := range sResp.Ims {
		ims[im.User] = im
	}
	return ims, nil
}

func (s *Slack) channelsList(ctx context.Context) (map[string]slackChannel, error) {
	data := url.Values{}
	data.Set("token", s.token)

	resp, err := s.doPost(ctx, slackURL+"/channels.list", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("faile to create channels.list request: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
		Channels []slackChannel
	}

	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %s", err)

	}
	if !sResp.Ok {
		return nil, fmt.Errorf("channels.list failed error:%s warning:%s", sResp.Error, sResp.Warning)
	}

	channels := make(map[string]slackChannel)
	for _, ch := range sResp.Channels {
		channels[ch.Id] = ch
	}
	return channels, nil
}

func (s *Slack) chatPostMessage(ctx context.Context, msg Message) error {
	data := url.Values{}
	data.Set("token", s.token)
	data.Set("channel", msg.Chat.ID)
	data.Set("text", msg.Text)
	data.Set("as_user", "true")
	if len(msg.Attachments) > 0 {
		attachments, err := json.Marshal(msg.Attachments)
		if err != nil {
			return fmt.Errorf("marshall attachments failed: %s", attachments)
		}
		data.Set("attachments", string(attachments))
	}
	if msg.Chat.Type == Thread {
		data.Set("thread_ts", msg.ReplyTo.ID)
	}

	resp, err := s.doPost(ctx, slackURL+"/chat.postMessage", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("chat.PostMessage request failed: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
	}
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return fmt.Errorf("failed to parse response: %s", err)
	}
	if !sResp.Ok {
		return fmt.Errorf("channels.list failed error:%s warning:%s", sResp.Error, sResp.Warning)
	}

	return nil
}

func (s *Slack) parseIncomingMessage(rawMsg []byte) (*Message, error) {
	log(Debug, fmt.Sprintf("incoming rawMsg:%s", rawMsg))

	var rawType struct {
		Type string
	}
	if err := json.Unmarshal(rawMsg, &rawType); err != nil {
		return nil, fmt.Errorf("failed parsing message type: %s", err)
	}

	if ignoreMessageType[rawType.Type] {
		// this type of message has specific structure, ignore for now
		return nil, nil
	}

	var raw struct {
		Type        string
		Channel     string
		User        string
		Username    string
		BotID       string
		Text        string
		Ts          string
		Attachments []Attachment
		SubType     string
		//SourceTeam string
		//Team string
	}
	if err := json.Unmarshal(rawMsg, &raw); err != nil {
		return nil, fmt.Errorf("failed parsing message type: %s", err)
	}

	var ts time.Time
	if raw.Ts != "" {
		timestamp, err := strconv.ParseFloat(raw.Ts, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %s", err)
		}
		ts = time.Unix(int64(timestamp), 0)
	}

	slackUser := s.idToMember[raw.User]
	user := User{
		ID:        raw.User,
		FirstName: slackUser.Profile.FirstName,
		LastName:  slackUser.Profile.LastName,
		Username:  slackUser.Name,
	}

	msg := Message{}
	chatType := Group
	if strings.HasPrefix(raw.Channel, "D") {
		chatType = Private
	}
	switch raw.Type {
	case "message":
		msg = Message{
			ID: raw.Ts,
			Chat: Chat{
				ID:   raw.Channel,
				Type: chatType,
			},
			From:        user,
			Date:        ts,
			Text:        raw.Text,
			Format:      Text,
			Attachments: raw.Attachments,
		}
		if raw.SubType == "bot_message" {
			msg.From.Username = raw.Username
			msg.From.ID = raw.BotID
		}
	}

	return &msg, nil
}

func (s *Slack) UserName() string {
	return s.name
}

func (s *Slack) imID(ctx context.Context, userIDorName string) (string, error) {
	if userIDorName == "" {
		return "", errors.New("empty username")
	}
	var userID = userIDorName
	if strings.HasPrefix(userIDorName, "@") {
		member, ok := s.userNameToMember[userID[1:]]
		if !ok {
			return "", fmt.Errorf("failed to get user id for %q", userID)
		}
		userID = member.ID
	}

	dm, ok := s.ims[userID]
	if !ok {
		data := url.Values{}
		data.Set("token", s.token)
		data.Set("user", userID)
		resp, err := s.doPost(ctx, slackURL+"/im.open", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
		if err != nil {
			return "", fmt.Errorf("failed to create connect request: %s", err)
		}
		defer resp.Body.Close()

		var sResp struct {
			slackResponse
			Channel struct {
				ID      string
				IsIM    bool
				User    string
				Created int64
			}
		}
		if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
			return "", fmt.Errorf("failed to parse response: %s", err)

		}
		if !sResp.Ok {
			return "", fmt.Errorf("failed to open IM for user %s error:%s warning:%s", userID, sResp.Error, sResp.Warning)
		}
		dm = slackIm{
			ID:      sResp.Channel.ID,
			IsIM:    sResp.Channel.IsIM,
			User:    sResp.Channel.User,
			Created: sResp.Channel.Created,
		}
		s.ims[userID] = dm
	}
	return dm.ID, nil
}

func (s *Slack) Mentioned(field string) bool {
	if !strings.HasPrefix(field, "<@") || !strings.HasSuffix(field, ">") {
		return false
	}

	return field[2:len(field)-1] == s.id
}

func (s *Slack) Mention(u User) string {
	return "<@" + u.ID + ">"
}

func (s *Slack) UserByName(username string) (User, bool) {
	if strings.HasPrefix(username, "@") {
		username = username[1:]
	}
	var u User
	slackUser, ok := s.userNameToMember[username]
	if !ok {
		return u, false
	}

	return User{
		ID:        slackUser.ID,
		FirstName: slackUser.Profile.FirstName,
		LastName:  slackUser.Profile.LastName,
		Username:  slackUser.Name,
	}, true
}

func (s *Slack) EmulateReceiveMessage(raw []byte) error {
	msg, err := s.parseIncomingMessage(raw)
	if err != nil {
		return fmt.Errorf("failed to parse message: %s", err)
	}
	if msg == nil {
		return nil
	}
	s.handler(msg)
	return nil
}

func (s *Slack) SetTopic(ctx context.Context, chatID string, topic string) error {
	err := s.channelSetTopic(ctx, chatID, topic)
	if err != nil {
		if slackErr, ok := err.(slackError); ok {
			// it might be private group
			if slackErr.ErrorMsg == "channel_not_found" {
				err = s.groupSetTopic(ctx, chatID, topic)
			}
		}
	}
	return err
}

func (s *Slack) channelSetTopic(ctx context.Context, chatID string, topic string) error {
	data := url.Values{}
	data.Set("token", s.token)
	data.Set("channel", chatID)
	data.Set("topic", topic)
	resp, err := s.doPost(ctx, slackURL+"/conversations.setTopic", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("chat.setTopic request failed: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
	}
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return fmt.Errorf("failed to parse response: %s", err)
	}
	if !sResp.Ok {
		return slackError{
			Message:    "channels.setTopic failed",
			ErrorMsg:   sResp.Error,
			WarningMsg: sResp.Warning,
		}
	}

	return nil
}

func (s *Slack) groupSetTopic(ctx context.Context, chatID string, topic string) error {
	data := url.Values{}
	data.Set("token", s.token)
	data.Set("channel", chatID)
	data.Set("topic", topic)
	resp, err := s.doPost(ctx, slackURL+"/groups.setTopic", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("groups.setTopic request failed: %s", err)
	}
	defer resp.Body.Close()

	var sResp struct {
		slackResponse
	}
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return fmt.Errorf("failed to parse response: %s", err)
	}
	if !sResp.Ok {
		return slackError{
			Message:    "groups.setTopic failed",
			ErrorMsg:   sResp.Error,
			WarningMsg: sResp.Warning,
		}

	}

	return nil
}

func (s *Slack) ChatInfo(ctx context.Context, chatID string) (ChatInfo, error) {
	ci, err := s.channelInfo(ctx, chatID)
	if err != nil {
		if slackErr, ok := err.(slackError); ok {
			// it might be private group
			if slackErr.ErrorMsg == "channel_not_found" {
				ci, err = s.groupInfo(ctx, chatID)
			}
		}
	}
	return ci, err
}

func (s *Slack) channelInfo(ctx context.Context, chatID string) (c ChatInfo, err error) {
	data := url.Values{}
	data.Set("token", s.token)
	data.Set("channel", chatID)

	resp, err := s.doPost(ctx, slackURL+"/conversations.info", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return c, fmt.Errorf("channel.info request failed: %s", err)
	}
	defer func() {
		//noinspection ALL
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()

	var sResp struct {
		slackResponse
		slackConversation `json:"channel"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return c, fmt.Errorf("failed to parse response: %s", err)
	}
	if !sResp.Ok {
		return c, slackError{
			Message:    "conversations.info failed",
			ErrorMsg:   sResp.Error,
			WarningMsg: sResp.Warning,
		}
	}

	c.ID = sResp.slackConversation.ID
	c.Title = sResp.slackConversation.Name
	c.Topic = sResp.slackConversation.Topic.Value
	c.Description = sResp.slackConversation.Purpose.Value

	return c, nil

}

func (s *Slack) groupInfo(ctx context.Context, chatID string) (c ChatInfo, err error) {
	data := url.Values{}
	data.Set("token", s.token)
	data.Set("channel", chatID)
	resp, err := s.doPost(ctx, slackURL+"/groups.info", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return c, fmt.Errorf("chat.info request failed: %s", err)
	}

	defer func() {
		//noinspection ALL
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()

	var sResp struct {
		slackResponse
		slackChannel `json:"group"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return c, fmt.Errorf("failed to parse response: %s", err)
	}
	if !sResp.Ok {
		return c, slackError{
			Message:    "groups.info failed",
			ErrorMsg:   sResp.Error,
			WarningMsg: sResp.Warning,
		}
	}

	c.ID = sResp.slackChannel.Id
	c.Title = sResp.slackChannel.Name
	c.Topic = sResp.slackChannel.Topic.Value
	c.Description = sResp.slackChannel.Purpose.Value

	return c, nil
}

func (s *Slack) UploadFile(ctx context.Context, chatID string, filename string, r io.Reader) error {

	// write multipart filed
	var b bytes.Buffer
	var fw io.Writer
	var err error
	w := multipart.NewWriter(&b)

	fw, err = w.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("failed to create multipart filed: %v", err)
	}
	if _, err := io.Copy(fw, r); err != nil {
		return fmt.Errorf("failed to read source file: %v", err)
	}

	// token
	if fw, err = w.CreateFormField("token"); err != nil {
		return fmt.Errorf("failed to add form field token: %v", err)
	}
	if _, err = fw.Write([]byte(s.token)); err != nil {
		return fmt.Errorf("failed to add form field token value: %v", err)
	}

	// channels
	if fw, err = w.CreateFormField("channels"); err != nil {
		return fmt.Errorf("failed to add form field channels: %v", err)
	}
	if _, err = fw.Write([]byte(chatID)); err != nil {
		return fmt.Errorf("failed to add form field channels value: %v", err)
	}

	//noinspection ALL
	w.Close()

	req, err := http.NewRequest("POST", slackURL+"/files.upload", &b)
	if err != nil {
		return fmt.Errorf("failed to create http request: %v", err)
	}

	req.Header.Set("Content-Type", w.FormDataContentType())
	req = req.WithContext(ctx)

	// curl -F file=@dramacat.gif -F channels=C024BE91L,#general -F token=xxxx-xxxxxxxxx-xxxx https://slack.com/api/files.upload
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("chat.info request failed: %s", err)
	}

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("got response status: %d", resp.StatusCode)
	}

	return nil
}

func (s *Slack) doPost(ctx context.Context, url string, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(ctx)

	return http.DefaultClient.Do(req)
}
