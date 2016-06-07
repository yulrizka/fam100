package main

import "testing"

func TestGet(t *testing.T) {
	chanID := "foo"
	roundPlayed := 100
	if err := incGame(chanID, roundPlayed); err != nil {
		t.Error(err)
	}

	_, nextRound, err := nextGame(chanID)
	if err != nil {
		t.Error(err)
	}
	if nextRound != roundPlayed+1 {
		t.Errorf("invalid next round")
	}
}
