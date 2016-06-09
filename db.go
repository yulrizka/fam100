package fam100

import (
	"fmt"
	"hash/crc32"

	"github.com/garyburd/redigo/redis"
)

var (
	redisPrefix = "fam100"

	channelKey, channelRankKey, playerKey, playerRankKey string
)

var DefaultDB RedisDB

func SetPrefix(prefix string) {
	redisPrefix = prefix
	channelKey = fmt.Sprintf("%s_channel_", redisPrefix)
	channelRankKey = fmt.Sprintf("%s_chan_rank_", redisPrefix)
	playerKey = fmt.Sprintf("%s_players", redisPrefix)
	playerRankKey = fmt.Sprintf("%s_player_rank", redisPrefix)
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
	v, err := r.conn.Do("GET", channelKey+chanID)
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

func (r *RedisDB) incRoundPlayed(chanID string) error {
	_, err := r.conn.Do("INCR", channelKey+chanID)

	return err
}

func (r RedisDB) saveScore(chanID string, scores rank) error {
	for _, score := range scores {
		r.conn.Send("HSET", playerKey, score.PlayerID, score.Name)
		r.conn.Send("ZINCRBY", playerRankKey, score.Score, score.PlayerID)
		r.conn.Send("ZINCRBY", channelRankKey+chanID, score.Score, score.PlayerID)
	}
	return r.conn.Flush()
}

func (r RedisDB) channelRanking(chanID string, limit int) (ranking rank, err error) {
	return r.getRanking(channelRankKey+chanID, limit)
}

func (r RedisDB) playerRanking(limit int) (rank, error) {
	return r.getRanking(playerRankKey, limit)
}

func (r RedisDB) getRanking(key string, limit int) (ranking rank, err error) {
	values, err := redis.Values(r.conn.Do("ZREVRANGE", key, 0, limit, "WITHSCORES"))
	if err != nil {
		return nil, err
	}

	ids := make([]interface{}, 0, len(values))
	ids = append(ids, playerKey)
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
	names, err := redis.Strings(r.conn.Do("HMGET", ids...))
	if err != nil {
		return nil, err
	}
	for i := range ranking {
		ranking[i].Name = names[i]
	}

	return ranking, nil
}

func (r RedisDB) playerScore(playerID PlayerID) (ps playerScore, err error) {
	return r.getScore(playerRankKey, playerID)
}

func (r RedisDB) playerChannelScore(chanID string, playerID PlayerID) (playerScore, error) {
	return r.getScore(channelRankKey+chanID, playerID)
}

func (r RedisDB) getScore(key string, playerID PlayerID) (ps playerScore, err error) {
	ps.PlayerID = playerID
	if ps.Name, err = redis.String(r.conn.Do("HGET", playerKey, playerID)); err != nil {
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
