package main

import (
	"net"
	"time"

	"github.com/cyberdelia/go-metrics-graphite"
	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
	"github.com/yulrizka/fam100"
)

var (
	gaugeInterval = 5 * time.Second

	playerJoinedCount    = metrics.NewRegisteredCounter("player.joined.count", metrics.DefaultRegistry)
	messagePrivateCount  = metrics.NewRegisteredCounter("message.private.count", metrics.DefaultRegistry)
	messageIncomingCount = metrics.NewRegisteredCounter("message.incoming.count", metrics.DefaultRegistry)
	messageOutgoingCount = metrics.NewRegisteredCounter("message.outgoing.count", metrics.DefaultRegistry)
	channelMigratedCount = metrics.NewRegisteredCounter("channel.migrated.count", metrics.DefaultRegistry)
	commandJoinCount     = metrics.NewRegisteredCounter("command.join.count", metrics.DefaultRegistry)
	commandScoreCount    = metrics.NewRegisteredCounter("command.score.count", metrics.DefaultRegistry)
	roundStartedCount    = metrics.NewRegisteredCounter("round.started.count", metrics.DefaultRegistry)
	roundFinishedCount   = metrics.NewRegisteredCounter("round.finished.count", metrics.DefaultRegistry)
	roundTimeoutCount    = metrics.NewRegisteredCounter("round.timeout.count", metrics.DefaultRegistry)
	gameStartedCount     = metrics.NewRegisteredCounter("game.started.count", metrics.DefaultRegistry)
	gameFinishedCount    = metrics.NewRegisteredCounter("game.finished.count", metrics.DefaultRegistry)
	answerCorrectCount   = metrics.NewRegisteredCounter("answer.correct.count", metrics.DefaultRegistry)

	channelTotal    = metrics.NewRegisteredGauge("channel.total", metrics.DefaultRegistry)
	playerTotal     = metrics.NewRegisteredGauge("player.total", metrics.DefaultRegistry)
	gameActiveTotal = metrics.NewRegisteredGauge("game.active.total", metrics.DefaultRegistry)
)

func initMetrics(b fam100Bot) {
	tick := time.Tick(gaugeInterval)
	if graphiteURL != "" {
		addr, err := net.ResolveTCPAddr("tcp", graphiteURL)
		if err != nil {
			log.Error("failed initializing graphite", zap.Error(err))
		} else {
			go graphite.Graphite(metrics.DefaultRegistry, 10e9, "metrics", addr)
		}
	}
	go func() {
		for range tick {
			n, err := fam100.DefaultDB.ChannelCount()
			if err != nil {
				log.Error("retrieving total channel failed", zap.Error(err))
				continue
			} else {
				channelTotal.Update(int64(n))
			}

			n, err = fam100.DefaultDB.PlayerCount()
			if err != nil {
				log.Error("retrieving total player failed", zap.Error(err))
				continue
			} else {
				playerTotal.Update(int64(n))
			}

			gameActiveTotal.Update(int64(len(b.channels)))
		}
	}()

}
