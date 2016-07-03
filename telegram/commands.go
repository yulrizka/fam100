package main

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

// scoreRequestDelayDuration is time before we serve score command
var scoreRequestDelayDuration = 10 * time.Second

var lastScoreRequest = make(map[string]time.Time)

// handleJoin handles "/join". Create game and start it if quorum
func (b *fam100Bot) cmdJoin(msg *bot.Message) bool {
	if b.handleDisabled(msg) {
		return true
	}

	commandJoinCount.Inc(1)
	chanID := msg.Chat.ID
	chanName := msg.Chat.Title
	ch, ok := b.channels[chanID]
	if !ok {
		playerJoinedCount.Inc(1)
		// create a new game
		quorumPlayer := map[string]bool{msg.From.ID: true}

		gameIn := make(chan fam100.Message, gameInBufferSize)
		game, err := fam100.NewGame(chanID, chanName, gameIn, b.gameOut)
		if err != nil {
			log.Error("creating a game", zap.String("chanID", chanID))
			return true
		}

		ch := &channel{ID: chanID, game: game, quorumPlayer: quorumPlayer}
		b.channels[chanID] = ch
		if len(ch.quorumPlayer) == minQuorum {
			ch.game.Start()
			return true
		}
		ch.startQuorumTimer(quorumWait, b.out)
		text := fmt.Sprintf(
			fam100.T("*%s* OK, butuh %d orang lagi, sisa waktu %s"),
			msg.From.FullName(),
			minQuorum-len(quorumPlayer),
			quorumWait,
		)
		b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: text, Format: bot.Markdown}
		log.Info("User joined", zap.String("playerID", msg.From.ID), zap.String("chanID", chanID))
		return true
	}

	if ch.game.State != fam100.Created || ch.quorumPlayer[msg.From.ID] {
		return true
	}

	// new player joined
	playerJoinedCount.Inc(1)
	ch.cancelTimer()
	ch.quorumPlayer[msg.From.ID] = true
	if len(ch.quorumPlayer) == minQuorum {
		ch.game.Start()
		return true
	}
	ch.startQuorumTimer(quorumWait, b.out)
	text := fmt.Sprintf(
		fam100.T("*%s* OK, butuh %d orang lagi, sisa waktu %s"),
		msg.From.FullName(),
		minQuorum-len(ch.quorumPlayer),
		quorumWait,
	)
	b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: text, Format: bot.Markdown}
	log.Info("User joined", zap.String("playerID", msg.From.ID), zap.String("chanID", chanID))

	return false
}

// handleJoin handles "/score" show top score for current channel
func (b *fam100Bot) cmdScore(msg *bot.Message) bool {
	if b.handleDisabled(msg) {
		return true
	}

	now := time.Now()
	if lastTime, ok := lastScoreRequest[msg.Chat.ID]; ok &&
		now.Before(lastTime.Add(scoreRequestDelayDuration)) {
		return false
	}
	lastScoreRequest[msg.Chat.ID] = now

	commandScoreCount.Inc(1)
	chanID := msg.Chat.ID
	rank, err := fam100.DefaultDB.ChannelRanking(chanID, 20)
	if err != nil {
		log.Error("getting channel ranking failed", zap.String("chanID", chanID), zap.Error(err))
		return true
	}

	text := "*Top Score:*" + formatRankText(rank)
	b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: text, Format: bot.Markdown}

	return true
}

func (b *fam100Bot) handleDisabled(msg *bot.Message) bool {
	chanID := msg.Chat.ID
	disabledMsg, _ := fam100.DefaultDB.ChannelConfig(chanID, "disabled", "")

	if disabledMsg != "" {
		log.Debug("channel is disabled", zap.String("chanID", chanID), zap.String("msg", disabledMsg))
		b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: disabledMsg, Format: bot.Markdown}
		return true
	}

	return false
}

func formatRoundText(msg fam100.QNAMessage) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[id: %d] %s?\n\n", msg.QuestionID, msg.QuestionText)
	for i, a := range msg.Answers {
		if a.Answered {
			if a.Highlight {
				fmt.Fprintf(w, "<b>%d. (%2d) %s \n  ✓ %s</b>\n", i+1, a.Score, a.Text, a.PlayerName)
			} else {
				fmt.Fprintf(w, "%d. (%2d) %s \n  ✓ <i>%s</i>\n", i+1, a.Score, a.Text, a.PlayerName)
			}
		} else {
			if msg.ShowUnanswered {
				fmt.Fprintf(w, "<b>%d. (%2d) %s \n</b>", i+1, a.Score, a.Text)
			} else {
				fmt.Fprintf(w, "%d. _________________________\n", i+1)
			}
		}
	}
	w.Flush()

	return b.String()
}

func formatRankText(rank fam100.Rank) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "\n")
	//lastPos := 0
	if len(rank) == 0 {
		fmt.Fprintf(w, fam100.T("Tidak ada"))
	} else {
		for i, ps := range rank {
			/*
				if lastPos != 0 && lastPos+1 != ps.Position {
					fmt.Fprintf(w, "...\n")
				}
			*/
			fmt.Fprintf(w, "%d. (%2d) %s\n", i+1, ps.Score, ps.Name)
		}
	}
	w.Flush()

	return b.String()
}

// cmdSay handles /say [chan_id] [message]
func (b *fam100Bot) cmdSay(msg *bot.Message) bool {
	fields := strings.SplitN(msg.Text, " ", 3)
	if len(fields) < 3 {
		b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: "usage: `/say [chanID] [message]`", Format: bot.Markdown}
		return true
	}
	chatID, text := fields[1], fields[2]
	b.out <- bot.Message{Chat: bot.Chat{ID: chatID}, Text: text, Format: bot.Markdown}

	return true
}

// cmdSay handles /channels [pattern]. empty pattern matches all
func (b *fam100Bot) cmdChannels(msg *bot.Message) bool {
	fields := strings.SplitN(msg.Text, " ", 2)
	if len(fields) < 2 || fields[1] == "" {
		b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: "usage: `/channels [regex pattern]`", Format: bot.Markdown}
		return true
	}

	channels, err := fam100.DefaultDB.Channels()
	if err != nil {
		b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: "channels failed. " + err.Error(), Format: bot.Markdown}
	}

	// filter out by regex
	r, err := regexp.Compile(fields[1])
	if err != nil {
		b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: "regex failed. " + err.Error(), Format: bot.Markdown}
	}

	results := make(map[string]string)
	for id, desc := range channels {
		if r.MatchString(desc) {
			results[id] = desc
		}
	}

	buf := bytes.NewBuffer(nil)
	for id, desc := range results {
		if r.MatchString(desc) {
			buf.WriteString("\n")
			buf.WriteString(id)
			buf.WriteString(" ")
			buf.WriteString(desc)
		}
	}

	text := fmt.Sprintf("found %d channels:", len(results))
	body := buf.String()
	if len(body) > 3000 {
		body = body[:3000]
		body = body[:strings.LastIndex(body, "\n")]
		body += "\n ... truncated"
	}

	b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: text + body, Format: bot.Text}

	return true
}

// cmdBroadcast handles /broadcast [msg]. Broadcast message to all channels
func (b *fam100Bot) cmdBroadcast(msg *bot.Message) bool {
	fields := strings.SplitN(msg.Text, " ", 2)
	if len(fields) < 2 || fields[1] == "" {
		b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: "usage: `/broadcast [message]`", Format: bot.Markdown}
		return true
	}

	channels, err := fam100.DefaultDB.Channels()
	if err != nil {
		b.out <- bot.Message{Chat: bot.Chat{ID: msg.Chat.ID}, Text: "channels failed. " + err.Error(), Format: bot.Markdown}
	}

	go func() {
		text := fields[1]
		for id := range channels {
			b.out <- bot.Message{Chat: bot.Chat{ID: id}, Text: text, Format: bot.Text}
			time.Sleep(1 * time.Second)
		}
	}()

	return true
}
