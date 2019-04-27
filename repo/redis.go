package repo

import (
	"fmt"
	"hash/crc32"
	"os"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pkg/errors"
	"github.com/rcrowley/go-metrics"
	"github.com/yulrizka/fam100/model"
)

func SetRedisPrefix(prefix string) {
	RedisPrefix = prefix
	// g: global, c: channel, p:player
	gStatsKey = fmt.Sprintf("%s_stats_", RedisPrefix)
	cStatsKey = fmt.Sprintf("%s_chan_stats_", RedisPrefix)
	pStatsKey = fmt.Sprintf("%s_player_stats_", RedisPrefix)
	cRankKey = fmt.Sprintf("%s_chan_rank_", RedisPrefix)

	cNameKey = fmt.Sprintf("%s_chan_name", RedisPrefix)
	pNameKey = fmt.Sprintf("%s_player_name", RedisPrefix)
	pRankKey = fmt.Sprintf("%s_player_rank", RedisPrefix)

	cConfigKey = fmt.Sprintf("%s_chan_config_", RedisPrefix)
	gConfigKey = fmt.Sprintf("%s_config", RedisPrefix)
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
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = ":6379"
	}

	r.pool = &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", addr)
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
	SetRedisPrefix(RedisPrefix)

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

func (r *RedisDB) NextGame(chanID string) (seed int64, nextRound int, err error) {
	defer dbNextGameTimer.UpdateSince(time.Now())

	seed = int64(crc32.ChecksumIEEE([]byte(chanID)))
	v, err := r.ChannelStats(chanID, "played")
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

func (r RedisDB) IncStats(key string) error {
	defer dbIncStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s", gStatsKey, key)
	_, err := conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) IncChannelStats(chanID, key string) error {
	defer dbIncChannelStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", cStatsKey, key, chanID)
	_, err := conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) IncPlayerStats(playerID model.PlayerID, key string) error {
	defer dbIncPlayerStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", pStatsKey, key, playerID)
	_, err := conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) Stats(key string) (interface{}, error) {
	defer dbStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s", gStatsKey, key)
	return conn.Do("GET", rkey)
}

func (r RedisDB) ChannelStats(chanID, key string) (interface{}, error) {
	defer dbChannelStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", cStatsKey, key, chanID)
	return conn.Do("GET", rkey)
}

func (r RedisDB) PlayerStats(playerID, key string) (interface{}, error) {
	defer dbPlayerStatsTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()

	rkey := fmt.Sprintf("%s%s_%s", pStatsKey, key, playerID)
	return conn.Do("GET", rkey)
}

func (r *RedisDB) IncRoundPlayed(chanID string) error {
	return r.IncChannelStats(chanID, "played")
}

func (r RedisDB) SaveScore(chanID, chanName string, scores model.Rank) error {
	defer dbSaveScoreTimer.UpdateSince(time.Now())

	conn := r.pool.Get()
	defer conn.Close()
	for _, score := range scores {
		if err := conn.Send("HSET", cNameKey, chanID, chanName); err != nil {
			return errors.Wrap(err, "failed to execute command")
		}
		if err := conn.Send("HSET", pNameKey, score.PlayerID, score.Name); err != nil {
			return errors.Wrap(err, "failed to execute command")
		}
		if err := conn.Send("ZINCRBY", pRankKey, score.Score, score.PlayerID); err != nil {
			return errors.Wrap(err, "failed to execute command")
		}
		if err := conn.Send("ZINCRBY", cRankKey+chanID, score.Score, score.PlayerID); err != nil {
			return errors.Wrap(err, "failed to execute command")
		}
	}
	return conn.Flush()
}

func (r RedisDB) ChannelRanking(chanID string, limit int) (ranking model.Rank, err error) {
	return r.getRanking(cRankKey+chanID, limit-1)
}

func (r RedisDB) PlayerRanking(limit int) (model.Rank, error) {
	return r.getRanking(pRankKey, limit-1)
}

func (r RedisDB) getRanking(key string, limit int) (ranking model.Rank, err error) {
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
		var ps model.PlayerScore
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

func (r RedisDB) PlayerScore(playerID model.PlayerID) (ps model.PlayerScore, err error) {
	return r.getScore(pRankKey, playerID)
}

func (r RedisDB) PlayerChannelScore(chanID string, playerID model.PlayerID) (model.PlayerScore, error) {
	return r.getScore(cRankKey+chanID, playerID)
}

func (r RedisDB) getScore(key string, playerID model.PlayerID) (ps model.PlayerScore, err error) {
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
