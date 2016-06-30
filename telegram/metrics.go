package main

import (
	"net"
	"runtime"
	"time"

	"github.com/cyberdelia/go-metrics-graphite"
	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
	"github.com/yulrizka/fam100"
)

var (
	gaugeInterval     = 5 * time.Second
	goCollectInterval = 20 * time.Second

	errorCount = metrics.NewRegisteredCounter("log.error", metrics.DefaultRegistry)

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

	// golang metrics
	alloc        = metrics.NewRegisteredGauge("memory.alloc", metrics.DefaultRegistry)
	totalAlloc   = metrics.NewRegisteredGauge("memory.totalAlloc", metrics.DefaultRegistry)
	sys          = metrics.NewRegisteredGauge("memory.sys", metrics.DefaultRegistry)
	lookups      = metrics.NewRegisteredGauge("memory.lookups", metrics.DefaultRegistry)
	mallocs      = metrics.NewRegisteredGauge("memory.mallocs", metrics.DefaultRegistry)
	frees        = metrics.NewRegisteredGauge("memory.frees", metrics.DefaultRegistry)
	heapAlloc    = metrics.NewRegisteredGauge("memory.heapAlloc", metrics.DefaultRegistry)
	heapSys      = metrics.NewRegisteredGauge("memory.heapSys", metrics.DefaultRegistry)
	heapIdle     = metrics.NewRegisteredGauge("memory.heapIdle", metrics.DefaultRegistry)
	heapInuse    = metrics.NewRegisteredGauge("memory.heapInuse", metrics.DefaultRegistry)
	heapReleased = metrics.NewRegisteredGauge("memory.heapReleased", metrics.DefaultRegistry)
	heapObjects  = metrics.NewRegisteredGauge("memory.heapObjects", metrics.DefaultRegistry)
	stackInuse   = metrics.NewRegisteredGauge("memory.stackInuse", metrics.DefaultRegistry)
	stackSys     = metrics.NewRegisteredGauge("memory.stackSys", metrics.DefaultRegistry)
	pauseTotalNs = metrics.NewRegisteredGauge("memory.pauseTotalNs", metrics.DefaultRegistry)
	numGC        = metrics.NewRegisteredGauge("memory.numGC", metrics.DefaultRegistry)
	numGoroutine = metrics.NewRegisteredGauge("go.NumGoroutine", metrics.DefaultRegistry)
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

	// collect memory statistics
	go func() {
		c := time.Tick(goCollectInterval)
		for range c {
			ms := &runtime.MemStats{}
			runtime.ReadMemStats(ms)

			alloc.Update(int64(ms.Alloc))
			totalAlloc.Update(int64(ms.TotalAlloc))
			sys.Update(int64(ms.Sys))
			lookups.Update(int64(ms.Lookups))
			mallocs.Update(int64(ms.Mallocs))
			frees.Update(int64(ms.Frees))
			heapAlloc.Update(int64(ms.HeapAlloc))
			heapSys.Update(int64(ms.HeapSys))
			heapIdle.Update(int64(ms.HeapIdle))
			heapInuse.Update(int64(ms.HeapInuse))
			heapReleased.Update(int64(ms.HeapReleased))
			heapObjects.Update(int64(ms.HeapObjects))
			stackInuse.Update(int64(ms.StackInuse))
			stackSys.Update(int64(ms.StackSys))
			pauseTotalNs.Update(int64(ms.PauseTotalNs))
			numGC.Update(int64(ms.NumGC))
			numGoroutine.Update(int64(runtime.NumGoroutine()))

		}
	}()

}
