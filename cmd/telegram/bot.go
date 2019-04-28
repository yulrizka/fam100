package main

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
	"github.com/yulrizka/fam100/model"
	"github.com/yulrizka/fam100/qna"
	"github.com/yulrizka/fam100/repo"
)

type fam100Bot struct {
	// channel to communicate with telegram
	in       chan interface{}
	out      chan bot.Message
	channels map[string]*channel
	name     string

	// channel to communicate with game
	gameOut chan fam100.Message
	quit    chan struct{}

	// question database
	qnaDB qna.Provider

	cl bot.Client
}

func (b *fam100Bot) Name() string {
	return b.name
}

func (b *fam100Bot) Init(ctx context.Context, out chan bot.Message, cl bot.Client) (err error) {
	b.cl = cl
	b.in = make(chan interface{}, telegramInBufferSize)
	b.out = out

	b.gameOut = make(chan fam100.Message, gameOutBufferSize)
	b.channels = make(map[string]*channel)

	go b.handleOutbox()
	go b.handleInbox()

	return nil
}

// handleInbox handles incoming chat message
func (b *fam100Bot) Handle(_ context.Context, rawMsg interface{}) (handled bool, modifiedMsg interface{}) {
	//log.Info("got raw message", zap.Object("msg", rawMsg))
	b.in <- rawMsg

	return true, rawMsg
}

// handleInbox handles incoming message from telegram
func (b *fam100Bot) handleInbox() {
	for {
		select {
		case <-b.quit:
			return
		case rawMsg := <-b.in:
			start := time.Now()
			if rawMsg == nil {
				log.Fatal("handleInbox input channel is closed")
			}
			messageIncomingCount.Inc(1)
			switch msg := rawMsg.(type) {
			case *bot.ChannelMigratedMessage:
				b.handleChannelMigration(msg)
				mainHandleMigrationTimer.UpdateSince(start)
				continue
			case *bot.Message:
				if msg.Date.Before(startedAt) {
					// ignore message that is received before the process started
					log.Debug("message before started at", zap.Object("msg", msg), zap.String("startedAt", startedAt.String()), zap.String("date", msg.Date.String()))
					continue
				}
				log.Debug("handleInbox got message", zap.Object("msg", msg))
				msgType := msg.Chat.Type

				var cmdHandler func(msg *bot.Message) bool
				var cmdMetric metrics.Timer

				// Handles private message to bot
				if msgType == bot.Private {
					messagePrivateCount.Inc(1)
					log.Debug("Got private message", zap.Object("msg", msg))
					if msg.From.ID == adminID {
						switch {
						case strings.HasPrefix(msg.Text, "/say"):
							cmdHandler, cmdMetric = b.cmdSay, mainHandleSayTimer
						case strings.HasPrefix(msg.Text, "/channels"):
							cmdHandler, cmdMetric = b.cmdChannels, mainHandleChannelsTimer
						case strings.HasPrefix(msg.Text, "/broadcast"):
							cmdHandler, cmdMetric = b.cmdBroadcast, mainHandleBrodcastTimer
						}

						if cmdHandler != nil {
							if cmdHandler(msg) {
								cmdMetric.UpdateSince(start)
							}
						}
					}
					mainHandlePrivateChatTimer.UpdateSince(start)
					mainHandleMessageTimer.UpdateSince(start)
					continue
				}

				// Handle public commands
				switch msg.Text {
				case "/join", "/join@" + b.name:
					cmdHandler, cmdMetric = b.cmdJoin, mainHandleJoinTimer
				case "/score", "/score@" + b.name:
					cmdHandler, cmdMetric = b.cmdScore, mainHandleScoreTimer
				case "/help", "/help@" + b.name:
					// disabling to reduce outgoing message
					// cmdHandler, cmdMetric = b.cmdHelp, mainHandleScoreTimer
					continue
				}

				if cmdHandler != nil {
					if cmdHandler(msg) {
						// command was handled
						cmdMetric.UpdateSince(start)
						mainHandleMessageTimer.UpdateSince(start)
						continue
					}
				}

				// handle answers text
				chanID := msg.Chat.ID
				ch, ok := b.channels[chanID]
				if chanID == "" || !ok {
					log.Debug("channels not found", zap.String("chanID", chanID), zap.Object("msg", msg))
					mainHandleNotFoundTimer.UpdateSince(start)
					mainHandleMessageTimer.UpdateSince(start)
					continue
				}
				if len(ch.quorumPlayer) < minQuorum {
					// ignore message if no game started or it's not quorum yet
					mainHandleMinQuorumTimer.UpdateSince(start)
					mainHandleMessageTimer.UpdateSince(start)
					continue
				}

				// pass message to the fam100 game package
				gameMsg := fam100.TextMessage{
					Player:     model.Player{ID: model.PlayerID(msg.From.ID), Name: msg.From.FullName()},
					Text:       msg.Text,
					ReceivedAt: msg.ReceivedAt,
				}

				startSendingAt := time.Now()
				ch.game.In <- gameMsg
				mainSendToGameTimer.UpdateSince(startSendingAt)

				log.Debug("sent to game", zap.String("chanID", chanID), zap.Object("msg", msg))
				mainHandleMessageTimer.UpdateSince(start)
			}

		case chanID := <-timeoutChan:
			// chan failed to get quorum
			delete(b.channels, chanID)
			text := fmt.Sprintf(fam100.T("Permainan dibatalkan, jumlah pemain tidak cukup  😞"))
			b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: text, Format: bot.Markdown, DiscardAfter: time.Now().Add(5 * time.Second)}
			log.Info("Quorum timeout", zap.String("chanID", chanID))

		case chanID := <-finishedChan:
			delete(b.channels, chanID)
		}
	}
}

// handleChannelMigration handles if channel is migrated from group -> supergroup (telegram specific)
func (b *fam100Bot) handleChannelMigration(msg *bot.ChannelMigratedMessage) bool {
	channelMigratedCount.Inc(1)
	chanID := msg.FromID
	if ch, exists := b.channels[chanID]; exists {
		// TODO migrate channel score
		newID := msg.ToID
		ch.ID = newID
		ch.game.ChanID = newID
		delete(b.channels, chanID)
		b.channels[newID] = ch
		log.Info("Channel migrated", zap.String("from", chanID), zap.String("to", newID))
	}

	return true
}

// handleOutbox handles outgoing message from game to telegram
func (b *fam100Bot) handleOutbox() {
	for {
		select {
		case <-b.quit:
			return
		case rawMsg := <-b.gameOut:

			sent := true
			switch msg := rawMsg.(type) {
			default:
				sent = false

			case fam100.StateMessage:
				switch msg.State {
				case fam100.RoundStarted:
					var text string
					if msg.Round == 1 {
						gameStartedCount.Inc(1)
						text = fmt.Sprintf(fam100.T("Game (id: %d) dimulai\n<b>siapapun boleh menjawab tanpa</b> /join\n"), msg.GameID)
					}
					roundStartedCount.Inc(1)
					text += fmt.Sprintf(fam100.T("Ronde %d dari %d"), msg.Round, fam100.RoundPerGame)
					text += "\n\n" + formatRoundText(msg.RoundText)
					b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML, Retry: 3}

				case fam100.RoundFinished:
					roundFinishedCount.Inc(1)

				case fam100.RoundTimeout:
					roundTimeoutCount.Inc(1)

				case fam100.Finished:
					gameFinishedCount.Inc(1)
					finishedChan <- msg.ChanID
				}

			case fam100.QNAMessage:
				text := formatRoundText(msg)

				outMsg := bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML}
				if !msg.ShowUnanswered {
					answerCorrectCount.Inc(1)
					outMsg.DiscardAfter = time.Now().Add(5 * time.Second)
				} else {
					// mesage at the end of timeout
				}
				b.out <- outMsg

			case fam100.RankMessage:
				text := formatRankText(msg.Rank)
				if msg.Final {
					text = fam100.T("<b>Final score</b>:") + text

					// show leader board, TOP 3 + current game players
					rank, err := repo.DefaultDB.ChannelRanking(msg.ChanID, 3)
					if err != nil {
						log.Error("getting channel ranking failed", zap.String("chanID", msg.ChanID), zap.Error(err))
						continue
					}
					lookup := make(map[model.PlayerID]bool)
					for _, v := range rank {
						lookup[v.PlayerID] = true
					}
					for _, v := range msg.Rank {
						if !lookup[v.PlayerID] {
							playerScore, err := repo.DefaultDB.PlayerChannelScore(msg.ChanID, v.PlayerID)
							if err != nil {
								continue
							}

							rank = append(rank, playerScore)
						}
					}
					sort.Sort(rank)
					text += "\n<b>Total Score</b>" + formatRankText(rank)

					text += fmt.Sprintf("\nFull Score <a href=\"http://labs.yulrizka.com/fam100/scores.html?c=%s\">Lihat disini</a>\n", msg.ChanID)
					text += fmt.Sprintf(fam100.T("\nGame selesai!"))
					motd, _ := messageOfTheDay(msg.ChanID)
					if motd != "" {
						text = fmt.Sprintf("%s\n\n%s", text, motd)
					}
				} else {
					text = fam100.T("Score sementara:") + text
				}
				b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML, Retry: 3}

			case fam100.TickMessage:
				if msg.TimeLeft == 30*time.Second || msg.TimeLeft == 10*time.Second {
					text := fmt.Sprintf(fam100.T("sisa waktu %s"), msg.TimeLeft)
					b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML, DiscardAfter: time.Now().Add(2 * time.Second)}
				}

			case fam100.TextMessage:
				b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: msg.Text}
			}

			if sent {
				messageOutgoingCount.Inc(1)
			}
		}
	}
}

// channel represents channels chat rooms
type channel struct {
	ID                string
	game              *fam100.Game
	quorumPlayer      map[string]bool
	players           map[string]string
	startedAt         time.Time
	cancelTimer       context.CancelFunc
	cancelNotifyTimer context.CancelFunc
}

func (c *channel) startQuorumTimer(wait time.Duration, out chan bot.Message) {
	var ctx context.Context
	ctx, c.cancelTimer = context.WithCancel(context.Background())
	go func() {
		endAt := time.Now().Add(quorumWait)
		notify := []int64{30}

		for {
			if len(notify) == 0 {
				timeoutChan <- c.ID
				return
			}
			timeLeft := time.Duration(notify[0]) * time.Second
			tickAt := endAt.Add(-timeLeft)
			notify = notify[1:]

			select {
			case <-ctx.Done(): //canceled
				return
			case <-time.After(tickAt.Sub(time.Now())):
				text := fmt.Sprintf(fam100.T("Waktu sisa %s"), timeLeft)
				out <- bot.Message{Chat: bot.Chat{ID: c.ID}, Text: text, Format: bot.Markdown, DiscardAfter: time.Now().Add(2 * time.Second)}
			}
		}
	}()
}

//TODO: refactor into simpler function with game context
func (c *channel) startQuorumNotifyTimer(wait time.Duration, out chan bot.Message) {
	var ctx context.Context
	ctx, c.cancelNotifyTimer = context.WithCancel(context.Background())
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			players := make([]string, 0, len(c.players))
			for _, p := range c.players {
				players = append(players, p)
			}

			text := fmt.Sprintf(
				fam100.T("<b>%s</b> OK, butuh %d orang lagi, sisa waktu %s"),
				escape(strings.Join(players, ", ")),
				minQuorum-len(c.quorumPlayer),
				quorumWait,
			)
			out <- bot.Message{Chat: bot.Chat{ID: c.ID}, Text: text, Format: bot.HTML, DiscardAfter: time.Now().Add(5 * time.Second)}
			c.cancelNotifyTimer = nil
		}
	}()
}

func messageOfTheDay(chanID string) (string, error) {
	msgStr, err := repo.DefaultDB.ChannelConfig(chanID, "motd", "")
	if err != nil || msgStr == "" {
		msgStr, err = repo.DefaultDB.GlobalConfig("motd", "")
	}
	if err != nil {
		return "", err
	}
	messages := strings.Split(msgStr, ";")

	return messages[rand.Intn(len(messages))], nil
}
