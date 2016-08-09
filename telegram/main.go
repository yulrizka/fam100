package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
	"golang.org/x/net/context"
)

var (
	log                  zap.Logger
	logLevel             int
	minQuorum            = 3 // minimum players to start game
	graphiteURL          = ""
	graphiteWebURL       = ""
	quorumWait           = 120
	telegramInBufferSize = 10000
	gameInBufferSize     = 10000
	gameOutBufferSize    = 10000
	defaultQuestionLimit = 0
	botName              = "fam100bot"
	startedAt            time.Time
	timeoutChan          = make(chan string, 10000)
	finishedChan         = make(chan string, 10000)
	adminID              = ""
	httpTimeout          = 10
	roundDuration        = 90
	blockProfileRate     = 0
	plugin               = fam100Bot{}
	outboxWorker         = 0
	maxActiveGame        = 400

	// compiled time information
	VERSION   = ""
	BUILDTIME = ""
)

type logger struct {
	zap.Logger
}

func (l logger) Error(msg string, fields ...zap.Field) {
	l.Logger.Error(msg, fields...)
	errorCount.Inc(1)
}

func init() {
	log = logger{zap.NewJSON(zap.AddCaller(), zap.AddStacks(zap.FatalLevel))}
	fam100.ExtraQuestionSeed = 1
}

func main() {
	flag.StringVar(&botName, "botname", "fam100bot", "bot name")
	flag.StringVar(&adminID, "admin", "", "admin id")
	flag.IntVar(&minQuorum, "quorum", 3, "minimal channel quorum")
	flag.StringVar(&graphiteURL, "graphite", "", "graphite url, empty to disable")
	flag.StringVar(&graphiteWebURL, "graphiteWeb", "", "graphite web url, empty to disable")
	flag.IntVar(&roundDuration, "roundDuration", 90, "round duration in second")
	flag.IntVar(&defaultQuestionLimit, "questionLimit", -1, "set default question limit")
	flag.IntVar(&blockProfileRate, "blockProfile", 0, "enable go routine blockProfile for profiling rate set to 1000000000 for sampling every sec")
	flag.IntVar(&httpTimeout, "httpTimeout", 10, "http timeout in Second")
	flag.IntVar(&outboxWorker, "outboxWorker", 0, "telegram outbox sender worker")
	flag.IntVar(&maxActiveGame, "maxActiveGame", 400, "maximum active game limit")
	flag.IntVar(&quorumWait, "quorumWaitSecond", 120, "waiting quorim time before canceled")
	logLevel := zap.LevelFlag("v", zap.InfoLevel, "log level: all, debug, info, warn, error, panic, fatal, none")
	flag.Parse()

	go func() {
		if blockProfileRate > 0 {
			runtime.SetBlockProfileRate(blockProfileRate)
			log.Info("runtime.BlockProfile is enabled", zap.Int("rate", blockProfileRate))
		}
		log.Info("http listener", zap.Error(http.ListenAndServe("localhost:5050", nil)))
	}()

	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, os.Interrupt, syscall.SIGTERM)

		<-sigchan
		postEvent("fam100 shutdown", "shutdown", fmt.Sprintf("shutdown version:%s buildtime:%s", VERSION, BUILDTIME))
		log.Info("STOPED", zap.String("version", VERSION), zap.String("buildtime", BUILDTIME))
		os.Exit(0)
	}()

	// setup logger
	log.SetLevel(*logLevel)
	bot.SetLogger(log)
	fam100.SetLogger(log)
	log.Info("Fam100 STARTED",
		zap.String("version", VERSION),
		zap.String("buildtime", BUILDTIME),
		zap.Int("maxActiveGame", maxActiveGame),
	)
	postEvent("startup", "startup", fmt.Sprintf("startup version:%s buildtime:%s", VERSION, BUILDTIME))

	key := os.Getenv("TELEGRAM_KEY")
	if key == "" {
		log.Fatal("TELEGRAM_KEY can not be empty")
	}
	http.DefaultClient.Timeout = time.Duration(httpTimeout) * time.Second
	fam100.RoundDuration = time.Duration(roundDuration) * time.Second
	gameQueue = make(chan struct{}, maxActiveGame)

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
	if defaultQuestionLimit >= 0 {
		fam100.DefaultQuestionLimit = defaultQuestionLimit
	}
	if outboxWorker > 0 {
		bot.OutboxWorker = outboxWorker
	}
	log.Info("Question limit ", zap.Int("fam100.DefaultQuestionLimit", fam100.DefaultQuestionLimit))

	defer func() {
		if r := recover(); r != nil {
			fam100.DefaultQuestionDB.Close()
			panic(r)
		}
		fam100.DefaultQuestionDB.Close()
	}()

	if err := fam100.DefaultDB.Init(); err != nil {
		log.Fatal("Failed loading DB", zap.Error(err))
	}
	startedAt = time.Now()
	telegram := bot.NewTelegram(key)
	if err := telegram.AddPlugin(&plugin); err != nil {
		log.Fatal("Failed AddPlugin", zap.Error(err))
	}
	initMetrics(plugin)
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
				if msgType == bot.Private {
					messagePrivateCount.Inc(1)
					log.Debug("Got private message", zap.Object("msg", msg))
					if msg.From.ID == adminID {
						switch {
						case strings.HasPrefix(msg.Text, "/say"):
							if b.cmdSay(msg) {
								mainHandleSayTimer.UpdateSince(start)
								mainHandleMessageTimer.UpdateSince(start)
								continue
							}
						case strings.HasPrefix(msg.Text, "/channels"):
							if b.cmdChannels(msg) {
								mainHandleChannelsTimer.UpdateSince(start)
								mainHandleMessageTimer.UpdateSince(start)
								continue
							}
						case strings.HasPrefix(msg.Text, "/broadcast"):
							if b.cmdBroadcast(msg) {
								mainHandleBrodcastTimer.UpdateSince(start)
								mainHandleMessageTimer.UpdateSince(start)
								continue
							}
						}
					}
					mainHandlePrivateChatTimer.UpdateSince(start)
					mainHandleMessageTimer.UpdateSince(start)
					continue
				}

				// ## Handle Commands ##
				switch msg.Text {
				case "/join", "/join@" + botName:
					if b.cmdJoin(msg) {
						mainHandleJoinTimer.UpdateSince(start)
						mainHandleMessageTimer.UpdateSince(start)
						continue
					}
				case "/score", "/score@" + botName:
					if b.cmdScore(msg) {
						mainHandleScoreTimer.UpdateSince(start)
						mainHandleMessageTimer.UpdateSince(start)
						continue
					}
				case "/help", "/help@" + botName:
					continue
					/*
						if b.cmdHelp(msg) {
							mainHandleHelpTimer.UpdateSince(start)
							mainHandleMessageTimer.UpdateSince(start)
							continue
						}*/
				}

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
					Player:     fam100.Player{ID: fam100.PlayerID(msg.From.ID), Name: msg.From.FullName()},
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

			sent := true
			switch msg := rawMsg.(type) {
			default:
				sent = false
				// TODO: log error

			case fam100.StateMessage:
				switch msg.State {
				case fam100.Started:
					gameCurrentActiveCount.Inc(1)
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
					<-gameQueue
					gameCurrentActiveCount.Dec(1)
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
					rank, err := fam100.DefaultDB.ChannelRanking(msg.ChanID, 3)
					if err != nil {
						log.Error("getting channel ranking failed", zap.String("chanID", msg.ChanID), zap.Error(err))
						continue
					}
					lookup := make(map[fam100.PlayerID]bool)
					for _, v := range rank {
						lookup[v.PlayerID] = true
					}
					for _, v := range msg.Rank {
						if !lookup[v.PlayerID] {
							playerScore, err := fam100.DefaultDB.PlayerChannelScore(msg.ChanID, v.PlayerID)
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
		endAt := time.Now().Add(time.Duration(quorumWait) * time.Second)
		notify := []int64{30}

		for {
			if len(notify) == 0 {
				timeoutChan <- c.ID
				<-gameQueue
				gameCurrentQuorumCount.Dec(1)
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
				time.Duration(quorumWait)*time.Second,
			)
			out <- bot.Message{Chat: bot.Chat{ID: c.ID}, Text: text, Format: bot.HTML, DiscardAfter: time.Now().Add(5 * time.Second)}
			c.cancelNotifyTimer = nil
		}
	}()
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

func messageOfTheDay(chanID string) (string, error) {
	msgStr, err := fam100.DefaultDB.ChannelConfig(chanID, "motd", "")
	if err != nil || msgStr == "" {
		msgStr, err = fam100.DefaultDB.GlobalConfig("motd", "")
	}
	if err != nil {
		return "", err
	}
	messages := strings.Split(msgStr, ";")

	return messages[rand.Intn(len(messages))], nil
}
