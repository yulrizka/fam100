package main

import (
	"fmt"
	"log"
	"os"

	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

var (
	minQuorum            = 2 // minimum players to start game
	telegramInBufferSize = 10000
	gameInBufferSize     = 10000
	gameOutBufferSize    = 10000
	botName              = "fam100bot"
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
			msgType := msg.Chat.Type
			if msgType != bot.Group && msgType != bot.SuperGroup {
				// For now only accept group message
				return
			}
			chID := msg.Chat.ID
			ch, ok := m.channels[chID]

			// TODO: check for command to create an new game
			if msg.Text == "/join@"+botName {
				if !ok {
					seed, next, err := nextGame(chID)
					if err != nil {
						log.Printf("ERROR creating new game, channel:%s, %s", chID, err)
						continue
					}
					// create a new game
					quorumPlayer := map[string]bool{msg.From.ID: true}
					m.channels[chID] = &channel{game: fam100.NewGame(chID, seed, next, m.gameIn, m.gameOut), quorumPlayer: quorumPlayer}
					text := fmt.Sprintf(fam100.T("%s ok, butuh %d orang lagi"), msg.From.FullName(), minQuorum-len(quorumPlayer))
					m.out <- bot.Message{Chat: bot.Chat{ID: chID, Type: bot.Group}, Text: text}
					continue

				} else {
					ch.quorumPlayer[msg.From.ID] = true
					if len(ch.quorumPlayer) == minQuorum {
						text := fmt.Sprintf(fam100.T("Game dimulai, siapapun boleh jawab tanpa harus /join"))
						m.out <- bot.Message{Chat: bot.Chat{ID: chID, Type: bot.Group}, Text: text}
						ch.game.Start()
						continue
					}
				}
			}

			if chID == "" || !ok {
				// ignore message since no game started for that channel
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
			if gameMsg, ok := rawMsg.(fam100.TextMessage); ok {
				m.out <- bot.Message{
					Chat: bot.Chat{ID: gameMsg.GameID, Type: bot.Group},
					Text: gameMsg.Text,
				}
			}
		}
	}
}
