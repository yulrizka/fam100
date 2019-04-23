package fam100

import "github.com/rcrowley/go-metrics"

var (
	// game metrics
	gameMsgProcessTimer = metrics.NewRegisteredTimer("game.processedMessage.ns", metrics.DefaultRegistry)
	gameServiceTimer    = metrics.NewRegisteredTimer("game.serviceTime.ns", metrics.DefaultRegistry)
	gameLatencyTimer    = metrics.NewRegisteredTimer("game.latency.ns", metrics.DefaultRegistry)
	gameFinishedTimer   = metrics.NewRegisteredTimer("game.finished.ns", metrics.DefaultRegistry)
	playerActive        = metrics.NewRegisteredGauge("player.active", metrics.DefaultRegistry)
)
