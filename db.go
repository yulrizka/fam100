package fam100

import (
	"fmt"
	"hash/crc32"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/rcrowley/go-metrics"
)

type db interface {
	Reset() error
	Init() (err error)
	ChannelRanking(chanID string, limit int) (ranking Rank, err error)
	ChannelCount() (total int, err error)
	Channels() (channels map[string]string, err error)
	ChannelConfig(chanID, key, defaultValue string) (config string, err error)
	GlobalConfig(key, defaultValue string) (config string, err error)

	PlayerCount() (total int, err error)
	PlayerChannelScore(chanID string, playerID PlayerID) (PlayerScore, error)

	// stats command
	incStats(key string) error
	incChannelStats(chanID, key string) error
	incPlayerStats(playerID PlayerID, key string) error
	stats(key string) (interface{}, error)
	channelStats(chanID, key string) (interface{}, error)
	playerStats(playerID, key string) (interface{}, error)

	nextGame(chanID string) (seed int64, nextRound int, err error)
	incRoundPlayed(chanID string) error

	// scores
	saveScore(chanID, chanName string, scores Rank) error
	playerRanking(limit int) (Rank, error)
	playerScore(playerID PlayerID) (ps PlayerScore, err error)
}

var (
	redisPrefix = "fam100"

	gStatsKey, cStatsKey, pStatsKey, cRankKey, pNameKey, pRankKey string
	cNameKey, cConfigKey, gConfigKey                              string
)

// DefaultDB default question database
var DefaultDB db

func init() {
	DefaultDB = new(RedisDB)
}

func SetRedisPrefix(prefix string) {
	redisPrefix = prefix
	// g: global, c: channel, p:player
	gStatsKey = fmt.Sprintf("%s_stats_", redisPrefix)
	cStatsKey = fmt.Sprintf("%s_chan_stats_", redisPrefix)
	pStatsKey = fmt.Sprintf("%s_player_stats_", redisPrefix)
	cRankKey = fmt.Sprintf("%s_chan_rank_", redisPrefix)

	cNameKey = fmt.Sprintf("%s_chan_name", redisPrefix)
	pNameKey = fmt.Sprintf("%s_player_name", redisPrefix)
	pRankKey = fmt.Sprintf("%s_player_rank", redisPrefix)

	cConfigKey = fmt.Sprintf("%s_chan_config_", redisPrefix)
	gConfigKey = fmt.Sprintf("%s_config", redisPrefix)
}

type RedisDB struct {
	pool *redis.Pool
}

func (r *RedisDB) Reset() error {
	conn := r.pool.Get()
	defer conn.Close()

	_, err := conn.Do("FLUSHALL")
	return err
}

func (r *RedisDB) Init() (err error) {
	r.pool = &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", ":6379")
			if err != nil {
				return nil, err
			}
			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
	if _, err := r.pool.Get().Do("PING"); err != nil {
		return err
	}
	SetRedisPrefix(redisPrefix)

	go func() {
		redisConnCount := metrics.NewRegisteredGauge("redis.pool.count", metrics.DefaultRegistry)
		tick := time.Tick(5 * time.Second)
		for range tick {
			redisConnCount.Update(int64(r.pool.ActiveCount()))
		}
	}()

	return nil
}

func (r *RedisDB) ChannelCount() (total int, err error) {
	defer dbChannelCountTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()
	return redis.Int(conn.Do("HLEN", cNameKey))
}

func (r *RedisDB) Channels() (channels map[string]string, err error) {
	defer dbChannelsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()
	return redis.StringMap(conn.Do("HGETALL", cNameKey))
}

func (r *RedisDB) ChannelConfig(chanID, key, defaultValue string) (config string, err error) {
	defer dbChannelConfigTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s", cConfigKey, chanID)
	config, err = redis.String(conn.Do("HGET", rkey, key))

	if err != nil || config == "" {
		return defaultValue, err
	}

	return config, nil
}

func (r *RedisDB) GlobalConfig(key, defaultValue string) (config string, err error) {
	defer dbGlobalConfigTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := gConfigKey
	config, err = redis.String(conn.Do("HGET", rkey, key))

	if err != nil || config == "" {
		return defaultValue, err
	}

	return config, nil
}

func (r *RedisDB) PlayerCount() (total int, err error) {
	defer dbPlayerCountTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	return redis.Int(conn.Do("HLEN", pNameKey))
}

func (r *RedisDB) nextGame(chanID string) (seed int64, nextRound int, err error) {
	defer dbNextGameTimer.UpdateSince(time.Now())

	seed = int64(crc32.ChecksumIEEE([]byte(chanID)))
	v, err := r.channelStats(chanID, "played")
	if err != nil {
		return 0, 0, err
	}
	if v == nil {
		return seed, 0, nil
	}
	nextRound, err = redis.Int(v, err)
	if err != nil {
		return 0, 0, err
	}

	return seed, nextRound + 1, nil
}

func (r RedisDB) incStats(key string) error {
	defer dbIncStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s", gStatsKey, key)
	_, err := conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) incChannelStats(chanID, key string) error {
	defer dbIncChannelStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", cStatsKey, key, chanID)
	_, err := conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) incPlayerStats(playerID PlayerID, key string) error {
	defer dbIncPlayerStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", pStatsKey, key, playerID)
	_, err := conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) stats(key string) (interface{}, error) {
	defer dbStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s", gStatsKey, key)
	return conn.Do("GET", rkey)
}

func (r RedisDB) channelStats(chanID, key string) (interface{}, error) {
	defer dbChannelStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", cStatsKey, key, chanID)
	return conn.Do("GET", rkey)
}

func (r RedisDB) playerStats(playerID, key string) (interface{}, error) {
	defer dbPlayerStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", pStatsKey, key, playerID)
	return conn.Do("GET", rkey)
}

func (r *RedisDB) incRoundPlayed(chanID string) error {
	return r.incChannelStats(chanID, "played")
}

func (r RedisDB) saveScore(chanID, chanName string, scores Rank) error {
	defer dbSaveScoreTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()
	for _, score := range scores {
		conn.Send("HSET", cNameKey, chanID, chanName)
		conn.Send("HSET", pNameKey, score.PlayerID, score.Name)
		conn.Send("ZINCRBY", pRankKey, score.Score, score.PlayerID)
		conn.Send("ZINCRBY", cRankKey+chanID, score.Score, score.PlayerID)
	}
	return conn.Flush()
}

func (r RedisDB) ChannelRanking(chanID string, limit int) (ranking Rank, err error) {
	return r.getRanking(cRankKey+chanID, limit-1)
}

func (r RedisDB) playerRanking(limit int) (Rank, error) {
	return r.getRanking(pRankKey, limit-1)
}

func (r RedisDB) getRanking(key string, limit int) (ranking Rank, err error) {
	defer dbGetRankingTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()
	if limit <= 0 {
		limit = -1
	}

	values, err := redis.Values(conn.Do("ZREVRANGE", key, 0, limit, "WITHSCORES"))
	if err != nil {
		return nil, err
	}

	ids := make([]interface{}, 0, len(values))
	ids = append(ids, pNameKey)
	pos := 0
	for len(values) > 0 {
		var ps PlayerScore
		values, err = redis.Scan(values, &ps.PlayerID, &ps.Score)
		if err != nil {
			return nil, err
		}
		pos++
		ps.Position = pos
		ids = append(ids, ps.PlayerID)
		ranking = append(ranking, ps)
	}

	// get all name
	if len(ranking) > 0 {
		names, err := redis.Strings(conn.Do("HMGET", ids...))
		if err != nil {
			return nil, err
		}
		for i := range ranking {
			ranking[i].Name = names[i]
		}
	}

	return ranking, nil
}

func (r RedisDB) playerScore(playerID PlayerID) (ps PlayerScore, err error) {
	return r.getScore(pRankKey, playerID)
}

func (r RedisDB) PlayerChannelScore(chanID string, playerID PlayerID) (PlayerScore, error) {
	return r.getScore(cRankKey+chanID, playerID)
}

func (r RedisDB) getScore(key string, playerID PlayerID) (ps PlayerScore, err error) {
	defer dbGetScoreTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	ps.PlayerID = playerID
	if ps.Name, err = redis.String(conn.Do("HGET", pNameKey, playerID)); err != nil {
		return ps, err
	}
	if ps.Score, err = redis.Int(conn.Do("ZSCORE", key, playerID)); err != nil {
		return ps, err
	}
	if ps.Position, err = redis.Int(conn.Do("ZREVRANK", key, playerID)); err != nil {
		return ps, err
	}

	return ps, nil
}

// MemoryDB stores data in non persistence way
type MemoryDB struct {
	Seed   int64
	played int
}

func (m *MemoryDB) Reset() error                                                      { return nil }
func (m *MemoryDB) Init() (err error)                                                 { return nil }
func (m *MemoryDB) ChannelRanking(chanID string, limit int) (ranking Rank, err error) { return nil, nil }
func (m *MemoryDB) ChannelCount() (total int, err error)                              { return 0, nil }
func (m *MemoryDB) Channels() (channels map[string]string, err error)                 { return nil, nil }
func (m *MemoryDB) ChannelConfig(chanID, key, defaultValue string) (string, error)    { return "", nil }
func (m *MemoryDB) GlobalConfig(key, defaultValue string) (string, error)             { return "", nil }
func (m *MemoryDB) PlayerCount() (total int, err error)                               { return 0, nil }
func (m *MemoryDB) incStats(key string) error                                         { return nil }
func (m *MemoryDB) incChannelStats(chanID, key string) error                          { return nil }
func (m *MemoryDB) incPlayerStats(playerID PlayerID, key string) error                { return nil }
func (m *MemoryDB) stats(key string) (interface{}, error)                             { return nil, nil }
func (m *MemoryDB) channelStats(chanID, key string) (interface{}, error)              { return nil, nil }
func (m *MemoryDB) playerStats(playerID, key string) (interface{}, error)             { return nil, nil }
func (m *MemoryDB) saveScore(chanID, chanName string, scores Rank) error              { return nil }
func (m *MemoryDB) playerRanking(limit int) (Rank, error)                             { return nil, nil }
func (m *MemoryDB) playerScore(playerID PlayerID) (ps PlayerScore, err error) {
	return PlayerScore{}, nil
}
func (m *MemoryDB) PlayerChannelScore(chanID string, playerID PlayerID) (PlayerScore, error) {
	return PlayerScore{}, nil
}

func (m *MemoryDB) nextGame(chanID string) (seed int64, nextRound int, err error) {
	return m.Seed, m.played + 1, nil
}
func (m *MemoryDB) incRoundPlayed(chanID string) error {
	m.played++
	return nil
}
