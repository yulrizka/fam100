package repo

import (
	"github.com/yulrizka/fam100/model"
)

type db interface {
	Reset() error
	Init() (err error)
	ChannelRanking(chanID string, limit int) (ranking model.Rank, err error)
	ChannelCount() (total int, err error)
	Channels() (channels map[string]string, err error)
	ChannelConfig(chanID, key, defaultValue string) (config string, err error)
	GlobalConfig(key, defaultValue string) (config string, err error)

	PlayerCount() (total int, err error)
	PlayerChannelScore(chanID string, playerID model.PlayerID) (model.PlayerScore, error)

	// Stats command
	IncStats(key string) error
	IncChannelStats(chanID, key string) error
	IncPlayerStats(playerID model.PlayerID, key string) error
	Stats(key string) (interface{}, error)
	ChannelStats(chanID, key string) (interface{}, error)
	PlayerStats(playerID, key string) (interface{}, error)

	NextGame(chanID string) (seed int64, nextRound int, err error)
	IncRoundPlayed(chanID string) error

	// scores
	SaveScore(chanID, chanName string, scores model.Rank) error
	PlayerRanking(limit int) (model.Rank, error)
	PlayerScore(playerID model.PlayerID) (ps model.PlayerScore, err error)
}

var (
	RedisPrefix = "fam100"

	gStatsKey, cStatsKey, pStatsKey, cRankKey, pNameKey, pRankKey string
	cNameKey, cConfigKey, gConfigKey                              string
)

// DefaultDB default question database
var DefaultDB db

func init() {
	DefaultDB = new(RedisDB)
}
