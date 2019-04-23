package repo

import "github.com/rcrowley/go-metrics"

var (
	// db metrics
	dbChannelCountTimer    = metrics.NewRegisteredTimer("db.channelCount.ns", metrics.DefaultRegistry)
	dbChannelsTimer        = metrics.NewRegisteredTimer("db.channels.ns", metrics.DefaultRegistry)
	dbChannelConfigTimer   = metrics.NewRegisteredTimer("db.channelConfig.ns", metrics.DefaultRegistry)
	dbGlobalConfigTimer    = metrics.NewRegisteredTimer("db.globalConfig.ns", metrics.DefaultRegistry)
	dbPlayerCountTimer     = metrics.NewRegisteredTimer("db.playerCount.ns", metrics.DefaultRegistry)
	dbNextGameTimer        = metrics.NewRegisteredTimer("db.NextGame.ns", metrics.DefaultRegistry)
	dbIncStatsTimer        = metrics.NewRegisteredTimer("db.IncStats.ns", metrics.DefaultRegistry)
	dbIncChannelStatsTimer = metrics.NewRegisteredTimer("db.IncChannelStats.ns", metrics.DefaultRegistry)
	dbIncPlayerStatsTimer  = metrics.NewRegisteredTimer("db.IncPlayerStats.ns", metrics.DefaultRegistry)
	dbStatsTimer           = metrics.NewRegisteredTimer("db.Stats.ns", metrics.DefaultRegistry)
	dbChannelStatsTimer    = metrics.NewRegisteredTimer("db.ChannelStats.ns", metrics.DefaultRegistry)
	dbPlayerStatsTimer     = metrics.NewRegisteredTimer("db.PlayerStats.ns", metrics.DefaultRegistry)
	dbSaveScoreTimer       = metrics.NewRegisteredTimer("db.SaveScore.ns", metrics.DefaultRegistry)
	dbGetRankingTimer      = metrics.NewRegisteredTimer("db.getRanking.ns", metrics.DefaultRegistry)
	dbGetScoreTimer        = metrics.NewRegisteredTimer("db.getScore.ns", metrics.DefaultRegistry)
)
