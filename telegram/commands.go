package main

import (
	"bufio"
	"bytes"
	"fmt"

	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

// handleJoin handles "/join". Create game and start it if quorum
func (b *fam100Bot) handleJoin(msg *bot.Message) bool {
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
func (b *fam100Bot) handleScore(msg *bot.Message) bool {
	commandScoreCount.Inc(1)
	chanID := msg.Chat.ID
	rank, err := fam100.DefaultDB.ChannelRanking(chanID, 100)
	if err != nil {
		log.Error("getting channel ranking failed", zap.String("chanID", chanID), zap.Error(err))
		return true
	}

	text := "*Top Score:*" + formatRankText(rank)
	b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: text, Format: bot.Markdown}

	return true
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
	if len(rank) == 0 {
		fmt.Fprintf(w, fam100.T("Tidak ada"))
	} else {
		for i, ps := range rank {
			fmt.Fprintf(w, "%d. (%2d) %s\n", i+1, ps.Score, ps.Name)
		}
	}
	w.Flush()

	return b.String()
}
