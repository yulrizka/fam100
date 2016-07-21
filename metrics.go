package fam100

import "github.com/rcrowley/go-metrics"

var (
	// game metrics
	gameMsgProcessTimer = metrics.NewRegisteredTimer("game.processedMessage.ns", metrics.DefaultRegistry)
	gameServiceTimer    = metrics.NewRegisteredTimer("game.serviceTime.ns", metrics.DefaultRegistry)
	gameLatencyTimer    = metrics.NewRegisteredTimer("game.latency.ns", metrics.DefaultRegistry)
	gameFinishedTimer   = metrics.NewRegisteredTimer("game.finished.ns", metrics.DefaultRegistry)
	playerActive        = metrics.NewRegisteredGauge("player.active", metrics.DefaultRegistry)

	// db metrics
	dbChannelCountTimer    = metrics.NewRegisteredTimer("db.channelCount.ns", metrics.DefaultRegistry)
	dbChannelsTimer        = metrics.NewRegisteredTimer("db.channels.ns", metrics.DefaultRegistry)
	dbChannelConfigTimer   = metrics.NewRegisteredTimer("db.channelConfig.ns", metrics.DefaultRegistry)
	dbGlobalConfigTimer    = metrics.NewRegisteredTimer("db.globalConfig.ns", metrics.DefaultRegistry)
	dbPlayerCountTimer     = metrics.NewRegisteredTimer("db.playerCount.ns", metrics.DefaultRegistry)
	dbNextGameTimer        = metrics.NewRegisteredTimer("db.nextGame.ns", metrics.DefaultRegistry)
	dbIncStatsTimer        = metrics.NewRegisteredTimer("db.incStats.ns", metrics.DefaultRegistry)
	dbIncChannelStatsTimer = metrics.NewRegisteredTimer("db.incChannelStats.ns", metrics.DefaultRegistry)
	dbIncPlayerStatsTimer  = metrics.NewRegisteredTimer("db.incPlayerStats.ns", metrics.DefaultRegistry)
	dbStatsTimer           = metrics.NewRegisteredTimer("db.stats.ns", metrics.DefaultRegistry)
	dbChannelStatsTimer    = metrics.NewRegisteredTimer("db.channelStats.ns", metrics.DefaultRegistry)
	dbPlayerStatsTimer     = metrics.NewRegisteredTimer("db.playerStats.ns", metrics.DefaultRegistry)
	dbSaveScoreTimer       = metrics.NewRegisteredTimer("db.saveScore.ns", metrics.DefaultRegistry)
	dbGetRankingTimer      = metrics.NewRegisteredTimer("db.getRanking.ns", metrics.DefaultRegistry)
	dbGetScoreTimer        = metrics.NewRegisteredTimer("db.getScore.ns", metrics.DefaultRegistry)
)
