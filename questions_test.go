package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if err := loadQuestion("fam100.db"); err != nil {
		panic(err)
	}
	retCode := m.Run()
	db.Close()
	os.Exit(retCode)
}

func TestGetQuestion(t *testing.T) {
	q, err := getQuestion(1)
	if err != nil {
		t.Error(err)
	}

	ans := q.answers[1]
	wCorrect, wScore, wIndex := true, ans.score, 1
	gCorrect, gScore, gIndex := q.checkAnswer(ans.text[0])
	if gCorrect != wCorrect || gScore != wScore {
		t.Errorf("want: correct %t got %t, score %d got %d, index %d got %d", wCorrect, gCorrect, wScore, gScore, wIndex, gIndex)
	}
}

func TestNextQuestion(t *testing.T) {
	_, err := nextQuestion("foo", 0)
	if err != nil {
		t.Error(err)
	}
}
