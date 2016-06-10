package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"time"

	"golang.org/x/net/context"

	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

var (
	log                  zap.Logger
	minQuorum            = 3 // minimum players to start game
	quorumWait           = 120 * time.Second
	telegramInBufferSize = 10000
	gameInBufferSize     = 10000
	gameOutBufferSize    = 10000
	botName              = "fam100bot"
	startedAt            time.Time

	timeoutChan = make(chan string, 10000)
)

func init() {
	log = zap.NewJSON()
}

func main() {
	key := os.Getenv("TELEGRAM_KEY")
	if key == "" {
		log.Fatal("TELEGRAM_KEY can not be empty")
	}
	if err := fam100.LoadQuestion("fam100.db"); err != nil {
		log.Fatal("Failed loading question DB", zap.Error(err))
	}
	if err := fam100.DefaultDB.Init(); err != nil {
		log.Fatal("Failed loading DB", zap.Error(err))
	}
	startedAt = time.Now()
	telegram := bot.NewTelegram(key)
	plugin := fam100Bot{}
	if err := telegram.AddPlugin(&plugin); err != nil {
		log.Fatal("Failed AddPlugin", zap.Error(err))
	}
	plugin.start()

	telegram.Start()
}

// channel represents channels chat rooms
type channel struct {
	ID           string
	game         *fam100.Game
	quorumPlayer map[string]bool
	startedAt    time.Time
	cancelTimer  context.CancelFunc
}

func (c *channel) startQuorumTimer(wait time.Duration, out chan bot.Message) {
	var ctx context.Context
	ctx, c.cancelTimer = context.WithCancel(context.Background())
	go func() {
		endAt := time.Now().Add(quorumWait)
		notify := []int64{60, 30, 15}

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
				out <- bot.Message{Chat: bot.Chat{ID: c.ID, Type: bot.Group}, Text: text, Format: bot.Markdown}
			}
		}
	}()
}

type fam100Bot struct {
	// channel to communicate with telegram
	in       chan *bot.Message
	out      chan bot.Message
	channels map[string]*channel

	// channel to communicate with game
	gameIn  chan fam100.Message
	gameOut chan fam100.Message
	quit    chan struct{}
}

func (*fam100Bot) Name() string {
	return "Fam100Bot"
}

func (m *fam100Bot) Init(out chan bot.Message) (in chan *bot.Message, err error) {
	m.in = make(chan *bot.Message, telegramInBufferSize)
	m.out = out
	m.gameIn = make(chan fam100.Message, gameInBufferSize)
	m.gameOut = make(chan fam100.Message, gameOutBufferSize)
	m.channels = make(map[string]*channel)
	m.quit = make(chan struct{})

	return m.in, nil
}

func (m *fam100Bot) start() {
	go m.handleOutbox()
	go m.handleInbox()
}

func (m *fam100Bot) stop() {
	close(m.quit)
}

func (m *fam100Bot) handleInbox() {
	for {
		select {
		case <-m.quit:
			return
		case msg := <-m.in:
			if msg == nil {
				return // closed channel
			}
			if msg.Date.Before(startedAt) {
				// ignore message that is received before the process started
				continue
			}

			msgType := msg.Chat.Type
			if msgType != bot.Group && msgType != bot.SuperGroup {
				// For now only accept group message
				return
			}
			chanID := msg.Chat.ID
			ch, ok := m.channels[chanID]

			if msg.Text == "/join" || msg.Text == "/join@"+botName {
				if !ok {
					// create a new game
					quorumPlayer := map[string]bool{msg.From.ID: true}
					game, err := fam100.NewGame(chanID, m.gameIn, m.gameOut)
					if err != nil {
						log.Error("creating a game", zap.String("chanID", chanID))
						continue
					}

					ch := &channel{ID: chanID, game: game, quorumPlayer: quorumPlayer}
					m.channels[chanID] = ch
					ch.startQuorumTimer(quorumWait, m.out)
					text := fmt.Sprintf(
						fam100.T("*%s* OK, butuh %d orang lagi, sisa waktu %s"),
						msg.From.FullName(),
						minQuorum-len(quorumPlayer),
						quorumWait,
					)
					m.out <- bot.Message{Chat: bot.Chat{ID: chanID, Type: bot.Group}, Text: text, Format: bot.Markdown}
					log.Info("User joined", zap.String("playerID", msg.From.ID), zap.String("chanID", chanID))
					continue
				} else {
					if ch.game.State != fam100.Created || ch.quorumPlayer[msg.From.ID] {
						continue
					}
					ch.cancelTimer()
					ch.quorumPlayer[msg.From.ID] = true
					if len(ch.quorumPlayer) == minQuorum {
						ch.game.Start()
						continue
					}
					ch.startQuorumTimer(quorumWait, m.out)
					text := fmt.Sprintf(
						fam100.T("*%s* OK, butuh %d orang lagi, sisa waktu %s"),
						msg.From.FullName(),
						minQuorum-len(ch.quorumPlayer),
						quorumWait,
					)
					m.out <- bot.Message{Chat: bot.Chat{ID: chanID, Type: bot.Group}, Text: text, Format: bot.Markdown}
					log.Info("User joined", zap.String("playerID", msg.From.ID), zap.String("chanID", chanID))
				}
			}

			if chanID == "" || !ok {
				// ignore message since no game started for that channel
				continue
			}

			if msg.Text == "/leave" || msg.Text == "/leave@"+botName {
				if _, ok := ch.quorumPlayer[msg.From.ID]; ok {
					delete(ch.quorumPlayer, msg.From.ID)
					text := fmt.Sprintf(fam100.T("*%s* OK,  😞"), msg.From.FullName())
					m.out <- bot.Message{Chat: bot.Chat{ID: chanID, Type: bot.Group}, Text: text, Format: bot.Markdown}
					log.Info("User left game", zap.String("playerID", msg.From.ID), zap.String("chanID", chanID))
				}
				continue
			}

			if len(ch.quorumPlayer) < minQuorum {
				continue
			}

			gameMsg := fam100.TextMessage{
				Player: fam100.Player{ID: fam100.PlayerID(msg.From.ID), Name: msg.From.FullName()},
				Text:   msg.Text,
			}
			ch.game.In <- gameMsg

		case chanID := <-timeoutChan:
			// chan failed to get quorum
			delete(m.channels, chanID)
			text := fmt.Sprintf(fam100.T("Permainan dibatalkan, jumlah pemain tidak cukup  😞"))
			m.out <- bot.Message{Chat: bot.Chat{ID: chanID, Type: bot.Group}, Text: text, Format: bot.Markdown}
			log.Info("Quorum timeout", zap.String("chanID", chanID))
		}
	}
}

func (m *fam100Bot) handleOutbox() {
	for {
		select {
		case <-m.quit:
			return
		case rawMsg := <-m.gameOut:

			switch msg := rawMsg.(type) {
			default:
				// TODO: log error

			case fam100.StateMessage:
				switch msg.State {
				case fam100.Started:
					text := fmt.Sprintf(fam100.T("Game dimulai, siapapun boleh menjawab tanpa `/join`"))
					m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: text, Format: bot.Markdown}

				case fam100.RoundStarted:
					text := fmt.Sprintf(fam100.T("Ronde %d dari %d"), msg.Round, fam100.RoundPerGame)
					text += "\n\n" + formatRoundText(msg.RoundText)
					m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: text, Format: bot.HTML}

				case fam100.Finished:
					delete(m.channels, msg.ChanID)
					text := fmt.Sprintf(fam100.T("Game selesai!"))
					m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: text, Format: bot.Markdown}
				}

			case fam100.RoundTextMessage:
				text := formatRoundText(msg)
				m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: text, Format: bot.HTML}

			case fam100.RankMessage:

				text := formatRankText(msg)
				if msg.Final {
					text = fam100.T("Final score:") + text
				} else {
					text = fam100.T("Score sementara:") + text
				}
				m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: text, Format: bot.HTML}

			case fam100.TickMessage:
				if msg.TimeLeft == 30*time.Second || msg.TimeLeft == 10*time.Second {
					text := fmt.Sprintf(fam100.T("sisa waktu %s"), msg.TimeLeft)
					m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: text, Format: bot.HTML}
				}

			case fam100.TextMessage:
				m.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID, Type: bot.Group}, Text: msg.Text}
			}
		}
	}
}

func formatRoundText(msg fam100.RoundTextMessage) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[id: %d] %s?\n\n", msg.QuestionID, msg.QuestionText)
	for i, a := range msg.Answers {
		if a.Answered {
			fmt.Fprintf(w, "%d. (%2d) %s \n  ✓ <i>%s</i>\n", i+1, a.Score, a.Text, a.PlayerName)
		} else {
			if msg.ShowUnanswered {
				fmt.Fprintf(w, "%d. (%2d) %s \n", i+1, a.Score, a.Text)
			} else {
				fmt.Fprintf(w, "%d. _________________________\n", i+1)
			}
		}
	}
	w.Flush()

	return b.String()
}

func formatRankText(msg fam100.RankMessage) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "\n")
	if len(msg.Rank) == 0 {
		fmt.Fprintf(w, fam100.T("Tidak ada"))
	} else {
		for i, ps := range msg.Rank {
			fmt.Fprintf(w, "%d. (%2d) %s\n", i+1, ps.Score, ps.Name)
		}
	}
	w.Flush()

	return b.String()
}
