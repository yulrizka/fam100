package fam100

import (
	"fmt"
	"hash/crc32"

	"github.com/garyburd/redigo/redis"
)

type db interface {
	Reset() error
	Init() (err error)

	incStats(key string) error
	incChannelStats(chanID, key string) error
	incPlayerStats(playerID PlayerID, key string) error
	stats(key string) (interface{}, error)
	channelStats(chanID, key string) (interface{}, error)
	playerStats(playerID, key string) (interface{}, error)

	nextGame(chanID string) (seed int64, nextRound int, err error)
	incRoundPlayed(chanID string) error

	saveScore(chanID string, scores Rank) error
	ChannelRanking(chanID string, limit int) (ranking Rank, err error)
	playerRanking(limit int) (Rank, error)
	playerScore(playerID PlayerID) (ps playerScore, err error)
	playerChannelScore(chanID string, playerID PlayerID) (playerScore, error)
}

var (
	redisPrefix = "fam100"

	gStatsKey, cStatsKey, pStatsKey, cRankKey, pNameKey, pRankKey string
)

var DefaultDB db

func init() {
	DefaultDB = new(RedisDB)
}

func SetPrefix(prefix string) {
	redisPrefix = prefix
	// g: global, c: channel, p:player
	gStatsKey = fmt.Sprintf("%s_stats_", redisPrefix)
	cStatsKey = fmt.Sprintf("%s_chan_stats_", redisPrefix)
	pStatsKey = fmt.Sprintf("%s_player_stats_", redisPrefix)
	cRankKey = fmt.Sprintf("%s_chan_rank_", redisPrefix)
	pNameKey = fmt.Sprintf("%s_player_name", redisPrefix)
	pRankKey = fmt.Sprintf("%s_player_rank", redisPrefix)
}

type RedisDB struct {
	conn redis.Conn
}

func (r *RedisDB) Reset() error {
	_, err := r.conn.Do("FLUSHALL")
	return err
}

func (r *RedisDB) Init() (err error) {
	r.conn, err = redis.Dial("tcp", ":6379")
	if err != nil {
		return err
	}
	SetPrefix(redisPrefix)

	return nil
}

func (r *RedisDB) nextGame(chanID string) (seed int64, nextRound int, err error) {
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
	rkey := fmt.Sprintf("%s%s", gStatsKey, key)
	_, err := r.conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) incChannelStats(chanID, key string) error {
	rkey := fmt.Sprintf("%s%s_%s", cStatsKey, key, chanID)
	_, err := r.conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) incPlayerStats(playerID PlayerID, key string) error {
	rkey := fmt.Sprintf("%s%s_%s", pStatsKey, key, playerID)
	_, err := r.conn.Do("INCR", rkey)

	return err
}

func (r RedisDB) stats(key string) (interface{}, error) {
	rkey := fmt.Sprintf("%s%s", gStatsKey, key)
	return r.conn.Do("GET", rkey)
}

func (r RedisDB) channelStats(chanID, key string) (interface{}, error) {
	rkey := fmt.Sprintf("%s%s_%s", cStatsKey, key, chanID)
	return r.conn.Do("GET", rkey)
}

func (r RedisDB) playerStats(playerID, key string) (interface{}, error) {
	rkey := fmt.Sprintf("%s%s_%s", pStatsKey, key, playerID)
	return r.conn.Do("GET", rkey)
}

func (r *RedisDB) incRoundPlayed(chanID string) error {
	return r.incChannelStats(chanID, "played")
}

func (r RedisDB) saveScore(chanID string, scores Rank) error {
	for _, score := range scores {
		r.conn.Send("HSET", pNameKey, score.PlayerID, score.Name)
		r.conn.Send("ZINCRBY", pRankKey, score.Score, score.PlayerID)
		r.conn.Send("ZINCRBY", cRankKey+chanID, score.Score, score.PlayerID)
	}
	return r.conn.Flush()
}

func (r RedisDB) ChannelRanking(chanID string, limit int) (ranking Rank, err error) {
	return r.getRanking(cRankKey+chanID, limit)
}

func (r RedisDB) playerRanking(limit int) (Rank, error) {
	return r.getRanking(pRankKey, limit)
}

func (r RedisDB) getRanking(key string, limit int) (ranking Rank, err error) {
	values, err := redis.Values(r.conn.Do("ZREVRANGE", key, 0, limit, "WITHSCORES"))
	if err != nil {
		return nil, err
	}

	ids := make([]interface{}, 0, len(values))
	ids = append(ids, pNameKey)
	pos := 0
	for len(values) > 0 {
		var ps playerScore
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
		names, err := redis.Strings(r.conn.Do("HMGET", ids...))
		if err != nil {
			return nil, err
		}
		for i := range ranking {
			ranking[i].Name = names[i]
		}
	}

	return ranking, nil
}

func (r RedisDB) playerScore(playerID PlayerID) (ps playerScore, err error) {
	return r.getScore(pRankKey, playerID)
}

func (r RedisDB) playerChannelScore(chanID string, playerID PlayerID) (playerScore, error) {
	return r.getScore(cRankKey+chanID, playerID)
}

func (r RedisDB) getScore(key string, playerID PlayerID) (ps playerScore, err error) {
	ps.PlayerID = playerID
	if ps.Name, err = redis.String(r.conn.Do("HGET", pNameKey, playerID)); err != nil {
		return ps, err
	}
	if ps.Score, err = redis.Int(r.conn.Do("ZSCORE", key, playerID)); err != nil {
		return ps, err
	}
	if ps.Position, err = redis.Int(r.conn.Do("ZRANK", key, playerID)); err != nil {
		return ps, err
	}

	return ps, nil
}

// MemoryDb stores data in non persistence way
type MemoryDB struct {
	Seed   int64
	played int
}

func (m *MemoryDB) Reset() error                                                      { return nil }
func (m *MemoryDB) Init() (err error)                                                 { return nil }
func (m *MemoryDB) incStats(key string) error                                         { return nil }
func (m *MemoryDB) incChannelStats(chanID, key string) error                          { return nil }
func (m *MemoryDB) incPlayerStats(playerID PlayerID, key string) error                { return nil }
func (m *MemoryDB) stats(key string) (interface{}, error)                             { return nil, nil }
func (m *MemoryDB) channelStats(chanID, key string) (interface{}, error)              { return nil, nil }
func (m *MemoryDB) playerStats(playerID, key string) (interface{}, error)             { return nil, nil }
func (m *MemoryDB) saveScore(chanID string, scores Rank) error                        { return nil }
func (m *MemoryDB) ChannelRanking(chanID string, limit int) (ranking Rank, err error) { return nil, nil }
func (m *MemoryDB) playerRanking(limit int) (Rank, error)                             { return nil, nil }
func (m *MemoryDB) playerScore(playerID PlayerID) (ps playerScore, err error) {
	return playerScore{}, nil
}
func (m *MemoryDB) playerChannelScore(chanID string, playerID PlayerID) (playerScore, error) {
	return playerScore{}, nil
}

func (m *MemoryDB) nextGame(chanID string) (seed int64, nextRound int, err error) {
	return m.Seed, m.played + 1, nil
}
func (m *MemoryDB) incRoundPlayed(chanID string) error {
	m.played++
	return nil
}
