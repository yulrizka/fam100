package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/cyberdelia/go-metrics-graphite"
	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
	"github.com/yulrizka/fam100/repo"
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
	inboxQueueSize  = metrics.NewRegisteredGauge("inboxQueue.size", metrics.DefaultRegistry)
	outboxQueueSize = metrics.NewRegisteredGauge("outboxQueue.size", metrics.DefaultRegistry)

	cmdJoinTimer  = metrics.NewRegisteredTimer("command.join.ns", metrics.DefaultRegistry)
	cmdScoreTimer = metrics.NewRegisteredTimer("command.score.ns", metrics.DefaultRegistry)
	cmdHelpTimer  = metrics.NewRegisteredTimer("command.help.ns", metrics.DefaultRegistry)

	mainHandleMigrationTimer = metrics.NewRegisteredTimer("main.handleMigration.ns", metrics.DefaultRegistry)
	mainHandleMessageTimer   = metrics.NewRegisteredTimer("main.handleMessage.ns", metrics.DefaultRegistry)
	mainSendToGameTimer      = metrics.NewRegisteredTimer("main.sendToGame.ns", metrics.DefaultRegistry)

	// Todo should be removed
	// handle say
	mainHandleSayTimer = metrics.NewRegisteredTimer("main.handleSay.ns", metrics.DefaultRegistry)
	// handle channles
	mainHandleChannelsTimer = metrics.NewRegisteredTimer("main.handleChannels.ns", metrics.DefaultRegistry)
	// handle broadcast
	mainHandleBrodcastTimer = metrics.NewRegisteredTimer("main.handleBrodcast.ns", metrics.DefaultRegistry)
	// handle join
	mainHandleJoinTimer = metrics.NewRegisteredTimer("main.handleJoin.ns", metrics.DefaultRegistry)
	// handle score
	mainHandleScoreTimer = metrics.NewRegisteredTimer("main.handleScore.ns", metrics.DefaultRegistry)
	// handle help
	mainHandleHelpTimer = metrics.NewRegisteredTimer("main.handleHelp.ns", metrics.DefaultRegistry)
	// handle privateChat
	mainHandlePrivateChatTimer = metrics.NewRegisteredTimer("main.handlePrivateChat.ns", metrics.DefaultRegistry)

	// hanle notFound
	mainHandleNotFoundTimer = metrics.NewRegisteredTimer("main.handleNotFound.ns", metrics.DefaultRegistry)
	// handle minQuorum
	mainHandleMinQuorumTimer = metrics.NewRegisteredTimer("main.handleMinQuorum.ns", metrics.DefaultRegistry)

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
			hostname, err := os.Hostname()
			if err != nil {
				log.Fatal("hostname lookup failed", zap.Error(err))
			}
			prefix := fmt.Sprintf("%s.%s", hostname, b.Name())
			go graphite.Graphite(metrics.DefaultRegistry, 10e9, prefix, addr)
		}
	}
	go func() {
		for range tick {
			n, err := repo.DefaultDB.ChannelCount()
			if err != nil {
				log.Error("retrieving total channel failed", zap.Error(err))
				continue
			} else {
				channelTotal.Update(int64(n))
			}

			n, err = repo.DefaultDB.PlayerCount()
			if err != nil {
				log.Error("retrieving total player failed", zap.Error(err))
				continue
			} else {
				playerTotal.Update(int64(n))
			}

			gameActiveTotal.Update(int64(len(b.channels)))
		}
	}()

	go func() {
		for range time.Tick(1 * time.Second) {
			inboxQueueSize.Update(int64(len(plugin.in)))
			outboxQueueSize.Update(int64(len(plugin.out)))
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

func postEvent(what, tags, data string) error {
	if graphiteWebURL == "" {
		return nil
	}

	payload := struct {
		What string `json:"what"`
		Tags string `json:"tags"`
		Data string `json:"data"`
	}{what, tags, data}

	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(payload)
	_, err := http.DefaultClient.Post(graphiteWebURL+"/events/", "application/json", &buf)
	if err != nil {
		log.Error("failed sending event to graphite", zap.Error(err))
	}

	return err
}
