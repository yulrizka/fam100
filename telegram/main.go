package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/net/context"

	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

var (
	log                  zap.Logger
	logLevel             int
	minQuorum            = 3 // minimum players to start game
	quorumWait           = 120 * time.Second
	telegramInBufferSize = 10000
	gameInBufferSize     = 10000
	gameOutBufferSize    = 10000
	botName              = "fam100bot"
	startedAt            time.Time
	timeoutChan          = make(chan string, 10000)
	finishedChan         = make(chan string, 10000)

	// compiled time information
	VERSION   = ""
	BUILDTIME = ""
)

func init() {
	log = zap.NewJSON(zap.AddCaller(), zap.AddStacks(zap.FatalLevel))
	fam100.ExtraQuestionSeed = 1
	fam100.RoundDuration = 90 * time.Second
}

func main() {
	flag.StringVar(&botName, "botname", "fam100bot", "bot name")
	flag.IntVar(&minQuorum, "quorum", 3, "minimal channel quorum")
	logLevel := zap.LevelFlag("v", zap.InfoLevel, "log level: all, debug, info, warn, error, panic, fatal, none")
	flag.Parse()

	// setup logger
	log.SetLevel(*logLevel)
	bot.SetLogger(log)
	fam100.SetLogger(log)
	log.Info("Fam100 STARTED", zap.String("version", VERSION), zap.String("buildtime", BUILDTIME))

	key := os.Getenv("TELEGRAM_KEY")
	if key == "" {
		log.Fatal("TELEGRAM_KEY can not be empty")
	}
	handleSignal()

	dbPath := "fam100.db"
	if path := os.Getenv("QUESTION_DB_PATH"); path != "" {
		dbPath = path
	}
	if n, err := fam100.InitQuestion(dbPath); err != nil {
		log.Fatal("Failed loading question DB", zap.Error(err))
	} else {
		log.Info("Question loaded", zap.Int("nQuestion", n))
	}
	defer func() {
		if r := recover(); r != nil {
			fam100.QuestionDB.Close()
			panic(r)
		}
		fam100.QuestionDB.Close()
	}()

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

type fam100Bot struct {
	// channel to communicate with telegram
	in       chan interface{}
	out      chan bot.Message
	channels map[string]*channel

	// channel to communicate with game
	gameOut chan fam100.Message
	quit    chan struct{}
}

func (*fam100Bot) Name() string {
	return "Fam100Bot"
}

func (b *fam100Bot) Init(out chan bot.Message) (in chan interface{}, err error) {
	b.in = make(chan interface{}, telegramInBufferSize)
	b.out = out
	b.gameOut = make(chan fam100.Message, gameOutBufferSize)
	b.channels = make(map[string]*channel)
	b.quit = make(chan struct{})

	return b.in, nil
}

func (b *fam100Bot) start() {
	go b.handleOutbox()
	go b.handleInbox()
}

func (b *fam100Bot) stop() {
	close(b.quit)
}

// handleInbox handles incomming chat message
func (b *fam100Bot) handleInbox() {
	for {
		select {
		case <-b.quit:
			return
		case rawMsg := <-b.in:
			if rawMsg == nil {
				log.Fatal("handleInbox input channel is closed")
			}
			switch msg := rawMsg.(type) {
			case *bot.ChannelMigratedMessage:
				b.handleChannelMigration(msg)
				continue
			case *bot.Message:
				if msg.Date.Before(startedAt) {
					// ignore message that is received before the process started
					log.Debug("message before started at", zap.Object("msg", msg), zap.String("startedAt", startedAt.String()), zap.String("date", msg.Date.String()))
					continue
				}
				log.Debug("handleInbox got message", zap.Object("msg", msg))
				msgType := msg.Chat.Type
				if msgType == bot.Private {
					// private message is not supported yet
					log.Debug("Got private message", zap.Object("msg", msg))
					continue
				}

				// ## Handle Commands ##
				switch msg.Text {
				case "/join", "/join@" + botName:
					if b.handleJoin(msg) {
						continue
					}
				case "/score", "/score@" + botName:
					if b.handleScore(msg) {
						continue
					}
				}

				chanID := msg.Chat.ID
				ch, ok := b.channels[chanID]
				if chanID == "" || !ok {
					log.Debug("channels not found", zap.String("chanID", chanID), zap.Object("msg", msg))
					continue
				}
				if len(ch.quorumPlayer) < minQuorum {
					// ignore message if no game started or it's not quorum yet
					continue
				}

				// pass message to the fam100 game package
				gameMsg := fam100.TextMessage{
					Player: fam100.Player{ID: fam100.PlayerID(msg.From.ID), Name: msg.From.FullName()},
					Text:   msg.Text,
				}
				ch.game.In <- gameMsg
				log.Debug("sent to game", zap.String("chanID", chanID), zap.Object("msg", msg))
			}

		case chanID := <-timeoutChan:
			// chan failed to get quorum
			delete(b.channels, chanID)
			text := fmt.Sprintf(fam100.T("Permainan dibatalkan, jumlah pemain tidak cukup  😞"))
			b.out <- bot.Message{Chat: bot.Chat{ID: chanID}, Text: text, Format: bot.Markdown}
			log.Info("Quorum timeout", zap.String("chanID", chanID))

		case chanID := <-finishedChan:
			delete(b.channels, chanID)
		}
	}
}

// handleJoin handles "/join". Create game and start it if quorum
func (b *fam100Bot) handleJoin(msg *bot.Message) bool {
	chanID := msg.Chat.ID
	ch, ok := b.channels[chanID]
	if !ok {
		// create a new game
		quorumPlayer := map[string]bool{msg.From.ID: true}

		gameIn := make(chan fam100.Message, gameInBufferSize)
		game, err := fam100.NewGame(chanID, gameIn, b.gameOut)
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

// handleChannelMigration handles if channel is migrated from group -> supergroup (telegram specific)
func (b *fam100Bot) handleChannelMigration(msg *bot.ChannelMigratedMessage) bool {
	chanID := msg.Chat.ID
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

// handleOutbox handles outgoing message from game package
func (b *fam100Bot) handleOutbox() {
	for {
		select {
		case <-b.quit:
			return
		case rawMsg := <-b.gameOut:

			switch msg := rawMsg.(type) {
			default:
				// TODO: log error

			case fam100.StateMessage:
				switch msg.State {
				case fam100.Started:
					text := fmt.Sprintf(fam100.T("Game dimulai, siapapun boleh menjawab tanpa `/join`"))
					b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.Markdown}

				case fam100.RoundStarted:
					text := fmt.Sprintf(fam100.T("Ronde %d dari %d"), msg.Round, fam100.RoundPerGame)
					text += "\n\n" + formatRoundText(msg.RoundText)
					b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML}

				case fam100.Finished:
					finishedChan <- msg.ChanID
					text := fmt.Sprintf(fam100.T("Game selesai!"))
					b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.Markdown}
				}

			case fam100.QNAMessage:
				text := formatRoundText(msg)
				b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML}

			case fam100.RankMessage:

				text := formatRankText(msg.Rank)
				if msg.Final {
					text = fam100.T("Final score:") + text
				} else {
					text = fam100.T("Score sementara:") + text
				}
				b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML}

			case fam100.TickMessage:
				if msg.TimeLeft == 30*time.Second || msg.TimeLeft == 10*time.Second {
					text := fmt.Sprintf(fam100.T("sisa waktu %s"), msg.TimeLeft)
					b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: text, Format: bot.HTML}
				}

			case fam100.TextMessage:
				b.out <- bot.Message{Chat: bot.Chat{ID: msg.ChanID}, Text: msg.Text}
			}
		}
	}
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
				out <- bot.Message{Chat: bot.Chat{ID: c.ID}, Text: text, Format: bot.Markdown}
			}
		}
	}()
}

func formatRoundText(msg fam100.QNAMessage) string {
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

func handleSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)

	var prev = log.Level()
	go func() {
		for {
			<-c
			if log.Level() == zap.DebugLevel {
				log.SetLevel(prev)
			} else {
				prev = log.Level()
				log.SetLevel(zap.DebugLevel)
			}
			log.Info("log level switched to", zap.String("level", log.Level().String()))
		}
	}()
}
