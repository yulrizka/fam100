package main

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/uber-go/zap"
	"github.com/yulrizka/fam100"
)

var (
	gaugeInterval = 5 * time.Second

	playerJoinedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "player_joined_count",
		Help:      "Number of player joined a game, not including duplicate",
	})
	messagePrivateCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "message_private_count",
		Help:      "Number of private message received",
	})
	messageIncomingCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "message_incoming_count",
		Help:      "Number of incoming message",
	})
	messageOutgoingCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "message_outgoing_count",
		Help:      "Number of outgoing message",
	})
	channelMigratedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "channel_migrated_count",
		Help:      "Number of channel migrated to supergroup (telegram)",
	})
	commandJoinCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "command_join_count",
		Help:      "Number of join command received",
	})
	commandScoreCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "command_score_count",
		Help:      "Number of score command received",
	})
	roundStartedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "round_started_count",
		Help:      "Number of round started",
	})
	roundFinishedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "round_finished_count",
		Help:      "Number of round finished (all question answered)",
	})
	roundTimeoutCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "round_timeout_count",
		Help:      "Number of round times ran out",
	})
	gameStartedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "game_started_count",
		Help:      "Number of game started",
	})
	gameFinishedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "game_finished_count",
		Help:      "Number of game finished",
	})
	answerCorrectCount = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "fam100",
		Name:      "answer_correct_count",
		Help:      "Number of correct answer",
	})

	channelTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: "fam100",
		Name:      "channel_total",
		Help:      "total channel count",
	})
	playerTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: "fam100",
		Name:      "player_total",
		Help:      "total player count",
	})
	gameActiveTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: "fam100",
		Name:      "game_active_total",
		Help:      "Total current active game",
	})
)

func initMetrics(b fam100Bot) {
	log.Level() // overcome stupid goimports importing wrong pacakge
	counter := []prometheus.Collector{
		playerJoinedCount,
		messagePrivateCount,
		messageIncomingCount,
		messageOutgoingCount,
		channelMigratedCount,
		commandJoinCount,
		commandScoreCount,
		roundStartedCount,
		roundFinishedCount,
		roundTimeoutCount,
		gameStartedCount,
		gameFinishedCount,
		answerCorrectCount,
		channelTotal,
		playerTotal,
		gameActiveTotal,
	}
	for _, c := range counter {
		if err := prometheus.Register(c); err != nil {
			log.Error("metric registration failed", zap.Error(err))
		}
	}

	tick := time.Tick(gaugeInterval)
	go func() {
		for range tick {
			n, err := fam100.DefaultDB.ChannelCount()
			if err != nil {
				log.Error("retriving total channel failed", zap.Error(err))
				continue
			} else {
				channelTotal.Set(float64(n))
			}

			n, err = fam100.DefaultDB.PlayerCount()
			if err != nil {
				log.Error("retriving total player failed", zap.Error(err))
				continue
			} else {
				playerTotal.Set(float64(n))
			}

			gameActiveTotal.Set(float64(len(b.channels)))
		}
	}()

	// http handle for prometheus metrics
	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe("127.0.0.1:8080", nil)
}
