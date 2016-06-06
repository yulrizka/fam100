package main

import (
	"fmt"
	"hash/crc32"

	"github.com/garyburd/redigo/redis"
)

var (
	redisPrefix = "fam100"
	conn        redis.Conn
)

func initRedis() (err error) {
	conn, err = redis.Dial("tcp", ":6379")
	return err
}

func nextGame(chanID string) (seed int64, nextRound int, err error) {
	key := fmt.Sprintf("%s_channel_%s", redisPrefix, chanID)
	seed = int64(crc32.ChecksumIEEE([]byte(chanID)))
	v, err := conn.Do("GET", key)
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

func finishGame(chanID string, roundPlayed int) error {
	key := fmt.Sprintf("%s_channel_%s", redisPrefix, chanID)
	_, err := conn.Do("SET", key, roundPlayed)

	return err
}

func addPlayerScore(chanID, playerID string, score int64) error {
	globalKey := fmt.Sprintf("%s_global_score_%s", redisPrefix, playerID)
	channelKey := fmt.Sprintf("%s_chan_score_%s_%s", redisPrefix, chanID, playerID)

	conn.Send("ZINCRBY", globalKey, score)
	conn.Send("ZINCRBY", channelKey, score)
	return conn.Flush()
}
