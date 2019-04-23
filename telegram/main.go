package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
	"github.com/yulrizka/fam100/repo"
)

var (
	log                  zap.Logger
	logLevel             int
	minQuorum            = 3 // minimum players to start game
	graphiteURL          = ""
	graphiteWebURL       = ""
	quorumWait           = 120 * time.Second
	telegramInBufferSize = 10000
	gameInBufferSize     = 10000
	gameOutBufferSize    = 10000
	defaultQuestionLimit = 0
	startedAt            time.Time
	timeoutChan          = make(chan string, 10000)
	finishedChan         = make(chan string, 10000)
	adminID              = ""
	httpTimeout          = 10
	roundDuration        = 90
	blockProfileRate     = 0
	plugin               = fam100Bot{}
	outboxWorker         = 0
	profile              = false
)

// compiled time information
var (
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
	level := zap.InfoLevel
	setupLogger(&level)
	fam100.ExtraQuestionSeed = 1
}

func setupLogger(level *zap.Level) {
	var encoder zap.Encoder
	switch strings.ToUpper(os.Getenv("LOG_FORMAT")) {
	case "JSON":
		encoder = zap.NewJSONEncoder()
	case "TEXT":
		encoder = zap.NewTextEncoder()
	default:
		encoder = zap.NewJSONEncoder()
	}

	// init bot.log
	bot.Log = func(record bot.LogRecord) {
		switch record.Level {
		case bot.Debug:
			log.Debug(record.Message)
		case bot.Warn:
			log.Warn(record.Message)
		case bot.Info:
			log.Info(record.Message)
		case bot.Error:
			log.Error(record.Message)
		}
	}

	log = logger{zap.New(encoder, zap.AddCaller(), zap.AddStacks(zap.ErrorLevel), level)}
}

func main() {
	flag.StringVar(&adminID, "admin", "", "admin id")
	flag.IntVar(&minQuorum, "quorum", 3, "minimal channel quorum")
	flag.StringVar(&graphiteURL, "graphite", "", "graphite url, empty to disable")
	flag.StringVar(&graphiteWebURL, "graphiteWeb", "", "graphite web url, empty to disable")
	flag.IntVar(&roundDuration, "roundDuration", 90, "round duration in second")
	flag.IntVar(&defaultQuestionLimit, "questionLimit", -1, "set default question limit")
	flag.IntVar(&blockProfileRate, "blockProfile", 0, "enable go routine blockProfile for profiling rate set to 1000000000 for sampling every sec")
	flag.IntVar(&httpTimeout, "httpTimeout", 10, "http timeout in Second")
	flag.IntVar(&outboxWorker, "outboxWorker", 0, "telegram outbox sender worker")
	flag.BoolVar(&profile, "profile", false, "open go http profiler endpoint")
	logLevel := zap.LevelFlag("v", zap.InfoLevel, "log level: all, debug, info, warn, error, panic, fatal, none")
	flag.Parse()

	ctx := context.Background()

	go func() {
		if !profile {
			return
		}
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
		err := postEvent("fam100 shutdown", "shutdown", fmt.Sprintf("shutdown version:%s buildtime:%s", VERSION, BUILDTIME))
		if err != nil {
			log.Error("post event failed", zap.Error(err))
		}

		log.Info("STOPED", zap.String("version", VERSION), zap.String("buildtime", BUILDTIME))
		os.Exit(0)
	}()

	// setup logger
	setupLogger(logLevel)
	fam100.SetLogger(log)
	log.Info("Fam100 STARTED", zap.String("version", VERSION), zap.String("buildtime", BUILDTIME))
	err := postEvent("startup", "startup", fmt.Sprintf("startup version:%s buildtime:%s", VERSION, BUILDTIME))
	if err != nil {
		log.Error("post event failed", zap.Error(err))
	}

	key := os.Getenv("TELEGRAM_KEY")
	if key == "" {
		log.Fatal("TELEGRAM_KEY can not be empty")
	}
	http.DefaultClient.Timeout = time.Duration(httpTimeout) * time.Second
	fam100.RoundDuration = time.Duration(roundDuration) * time.Second

	dbPath := "fam100.db"
	if path := os.Getenv("QUESTION_DB_PATH"); path != "" {
		dbPath = path
	}
	log.Info("loading question DB", zap.String("path", dbPath))
	if n, err := fam100.InitQuestion(dbPath); err != nil {
		log.Fatal("Failed loading question DB", zap.String("path", dbPath), zap.Error(err))
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

	if err := repo.DefaultDB.Init(); err != nil {
		log.Fatal("Failed loading DB", zap.Error(err))
	}
	startedAt = time.Now()
	telegram, err := bot.NewTelegram(ctx, key)
	if err != nil {
		log.Fatal("telegram failed", zap.Error(err))
	}
	plugin.name = telegram.UserName()
	log.Info("Bot started", zap.String("name", plugin.name))

	err = telegram.AddPlugins([]bot.Plugin{&plugin}...)
	if err != nil {
		log.Fatal("Failed AddPlugin", zap.Error(err))
	}
	initMetrics(plugin)

	err = telegram.Start(ctx)
	if err != nil {
		log.Fatal("failed to start telegram", zap.Error(err))
	}

}
