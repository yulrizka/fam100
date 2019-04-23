package repo

import (
	"testing"

	"github.com/yulrizka/fam100/model"
)

func TestSaveScore(t *testing.T) {
	ranking := model.Rank{
		{PlayerID: "ID1", Name: "Name 1", Score: 15},
		{PlayerID: "ID2", Name: "Name 2", Score: 14},
		{PlayerID: "ID3", Name: "Name 3", Score: 13},
		{PlayerID: "ID4", Name: "Name 4", Score: 12},
		{PlayerID: "ID5", Name: "Name 5", Score: 11},
	}
	r := new(RedisDB)
	err := r.Init()
	if err != nil {
		t.Fatal(err)
	}

	chanID := "one"
	chanName := "one channel"
	for i := 0; i < 2; i++ {
		if err := r.SaveScore(chanID, chanName, ranking); err != nil {
			t.Error(err)
		}
	}

	// test ranking in specific channel
	chanRank, err := r.ChannelRanking(chanID, 100)
	if err != nil {
		t.Error(err)
	}
	for i, ps := range chanRank {
		if want, got := ranking[i].PlayerID, ps.PlayerID; want != got {
			t.Errorf("playerID, want %s got %s", want, got)
		}
		if want, got := ranking[i].Name, ps.Name; want != got {
			t.Errorf("playerID, want %s got %s", want, got)
		}
		if want, got := 2*ranking[i].Score, ps.Score; want != got {
			t.Errorf("playerID, want %d got %d", want, got)
		}
	}

	// test player rank
	chanID2 := "two"
	chanName2 := "two channel"
	if err := r.SaveScore(chanID2, chanName2, ranking); err != nil {
		t.Error(err)
	}
	playerRank, err := r.PlayerRanking(100)
	if err != nil {
		t.Error(err)
	}
	for i, ps := range playerRank {
		if want, got := ranking[i].PlayerID, ps.PlayerID; want != got {
			t.Errorf("playerID, want %s got %s", want, got)
		}
		if want, got := ranking[i].Name, ps.Name; want != got {
			t.Errorf("playerID, want %s got %s", want, got)
		}
		if want, got := 3*ranking[i].Score, ps.Score; want != got {
			t.Errorf("playerID, want %d got %d", want, got)
		}
	}

	// test PlayerScore
	var pid model.PlayerID = "ID1"
	ps, err := r.PlayerScore(pid)
	if err != nil {
		t.Error(err)
	}
	if want, got := pid, ps.PlayerID; want != got {
		t.Errorf("playerID, want %s got %s", want, got)
	}
	if want, got := "Name 1", ps.Name; want != got {
		t.Errorf("playerID, want %s got %s", want, got)
	}
	if want, got := 45, ps.Score; want != got {
		t.Errorf("playerID, want %d got %d", want, got)
	}

	// test playerChannelScore
	ps, err = r.PlayerChannelScore(chanID, pid)
	if err != nil {
		t.Error(err)
	}
	if want, got := pid, ps.PlayerID; want != got {
		t.Errorf("playerID, want %s got %s", want, got)
	}
	if want, got := "Name 1", ps.Name; want != got {
		t.Errorf("playerID, want %s got %s", want, got)
	}
	if want, got := 30, ps.Score; want != got {
		t.Errorf("playerID, want %d got %d", want, got)
	}
}
