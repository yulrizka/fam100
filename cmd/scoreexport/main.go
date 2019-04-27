package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/yulrizka/fam100"
)

var (
	outdir       = "scores"
	reset        = false
	overrideWeek = -1
)

type scoreFile struct {
	ChanID      string                 `json:"chanID"`
	LastUpdated string                 `json:"lastUpdated"`
	Total       fam100.Rank            `json:"total"`
	Rank        map[string]fam100.Rank `json:"rank"`
}

func (sf scoreFile) write() error {
	fileName := path.Join(outdir, sf.ChanID) + ".json"
	fileName = strings.Replace(fileName, "-", "!", -1)
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(file)
	enc.Encode(sf)
	if err := file.Close(); err != nil {
		log.Fatal(err)
	}

	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.StringVar(&outdir, "outdir", "scores", "output directory results")
	flag.BoolVar(&reset, "reset", false, "reset current week score")
	flag.IntVar(&overrideWeek, "week", -1, "override week")

	if outdir == "" {
		log.Fatal("outdir cannot be empty")
	}
	flag.Parse()
	year, week := time.Now().ISOWeek()
	if overrideWeek > 0 {
		week = overrideWeek
	}
	currentWeekKey := fmt.Sprintf("%d-%d", year, week)
	lastUpdated := time.Now().Format(time.RFC3339)

	// get all score from redis
	conn, err := redis.Dial("tcp", ":6379")
	if err != nil {
		log.Fatal(err)
	}

	keys, err := redis.ByteSlices(conn.Do("keys", "fam100_chan_rank_*"))
	if err != nil {
		log.Fatal(err)
	}

	if err := fam100.DefaultDB.Init(); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(outdir, 0744); err != nil {
		log.Fatal(err)
	}

	for _, key := range keys {
		chanID := strings.TrimPrefix(string(key), "fam100_chan_rank_")
		if chanID == "" {
			log.Printf("WARNING empty key, %s skipping\n", key)
			continue
		}

		// read total
		sf, err := readFile(chanID)
		if err != nil {
			log.Printf("ERROR, failed loading file, chanID %s", chanID)
			continue
		}

		// subtract total from current week
		total := sf.Total
		currentWeek := sf.Rank[currentWeekKey]
		total = total.Subtract(currentWeek)

		// update total from new currentWeek data
		currentWeek, err = fam100.DefaultDB.ChannelRanking(chanID, 0)
		if err != nil {
			log.Fatal(err)
		}
		total = total.Add(currentWeek)

		// write total & current week
		sf.ChanID = chanID
		sf.Total = total
		sf.Rank[currentWeekKey] = currentWeek
		sf.LastUpdated = lastUpdated
		if err := sf.write(); err != nil {
			log.Fatal(err)
		}

		// reset current week data if reset flag is true
		if reset {
			if _, err := conn.Do("DEL", key); err != nil {
				log.Panic(err)
			}
		}
	}
}

func readFile(chanID string) (scoreFile, error) {
	fileName := path.Join(outdir, chanID) + ".json"
	fileName = strings.Replace(fileName, "-", "!", -1)
	file, err := os.Open(fileName)
	if err != nil {
		log.Printf("WARNING: chanID:%s,  %s, crating new file", chanID, err)
		return scoreFile{Rank: make(map[string]fam100.Rank)}, nil
	}

	dec := json.NewDecoder(file)
	var sf scoreFile
	err = dec.Decode(&sf)

	return sf, err
}
