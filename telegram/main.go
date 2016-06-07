package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

var (
	minQuorum            = 2 // minimum players to start game
	telegramInBufferSize = 10000
	gameInBufferSize     = 10000
	gameOutBufferSize    = 10000
	botName              = "fam100bot"
	startedAt            time.Time
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
}

func main() {
	key := os.Getenv("TELEGRAM_KEY")
	if key == "" {
		panic("TELEGRAM_KEY can not be empty")
	}
	if err := fam100.LoadQuestion("fam100.db"); err != nil {
		panic(err)
	}
	if err := initRedis(); err != nil {
		panic(err)
	}
	startedAt = time.Now()
	telegram := bot.NewTelegram(key)
	plugin := fam100Bot{}
	if err := telegram.AddPlugin(&plugin); err != nil {
		panic(err)
	}
	plugin.start()

	telegram.Start()
}

// channel represents channels chat rooms
type channel struct {
	game         *fam100.Game
	quorumPlayer map[string]bool
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
			chID := msg.Chat.ID
			ch, ok := m.channels[chID]

			if msg.Text == "/join" || msg.Text == "/join@"+botName {
				if !ok {
					seed, next, err := nextGame(chID)
					if err != nil {
						log.Printf("ERROR creating new game, channel:%s, %s", chID, err)
						continue
					}
					// create a new game
					quorumPlayer := map[string]bool{msg.From.ID: true}
					m.channels[chID] = &channel{game: fam100.NewGame(chID, seed, next, m.gameIn, m.gameOut), quorumPlayer: quorumPlayer}
					text := fmt.Sprintf(fam100.T("*%s* OK, butuh %d orang lagi"), msg.From.FullName(), minQuorum-len(quorumPlayer))
					m.out <- bot.Message{Chat: bot.Chat{ID: chID, Type: bot.Group}, Text: text, Format: bot.Markdown}
					continue

				} else {
					ch.quorumPlayer[msg.From.ID] = true
					if len(ch.quorumPlayer) == minQuorum {
						ch.game.Start()
						continue
					}
				}
			}

			if chID == "" || !ok {
				// ignore message since no game started for that channel
				continue
			}

			if msg.Text == "/leave" || msg.Text == "/leave@"+botName {
				if _, ok := ch.quorumPlayer[msg.From.ID]; ok {
					delete(ch.quorumPlayer, msg.From.ID)
					text := fmt.Sprintf(fam100.T("*%s* OK,  😞"), msg.From.FullName())
					m.out <- bot.Message{Chat: bot.Chat{ID: chID, Type: bot.Group}, Text: text, Format: bot.Markdown}
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
					m.out <- bot.Message{Chat: bot.Chat{ID: msg.GameID, Type: bot.Group}, Text: text, Format: bot.Markdown}
				case fam100.RoundStarted:
					text := fmt.Sprintf(fam100.T("Ronde %d dari %d"), msg.Round, fam100.RoundPerGame)
					m.out <- bot.Message{Chat: bot.Chat{ID: msg.GameID, Type: bot.Group}, Text: text, Format: bot.Markdown}
				}

			case fam100.RoundTextMessage:
				m.out <- formatRoundText(msg)

			case fam100.TextMessage:
				m.out <- bot.Message{
					Chat: bot.Chat{ID: msg.GameID, Type: bot.Group},
					Text: msg.Text,
				}
			}
		}
	}
}

func formatRoundText(msg fam100.RoundTextMessage) bot.Message {

	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[id: %d] %s?\n\n", msg.QuestionID, msg.QuestionText)
	for i, a := range msg.Answers {
		if a.Answered {
			fmt.Fprintf(w, "%d. %-30s [ %2d ] - %s\n", i+1, a.Text, a.Score, a.PlayerName)
		} else {
			if msg.ShowUnanswered {
				fmt.Fprintf(w, "%d. %-30s [ %2d ]\n", i+1, a.Text, a.Score)
			} else {
				fmt.Fprintf(w, "%d. _________________________\n", i+1)
			}
		}
	}
	w.Flush()
	return bot.Message{
		Chat: bot.Chat{ID: msg.GameID, Type: bot.Group},
		Text: b.String(),
	}
}
