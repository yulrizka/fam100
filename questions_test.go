package fam100

import (
	"strings"
	"testing"
)

func TestGetQuestion(t *testing.T) {
	q, err := GetQuestion("1")
	if err != nil {
		t.Error(err)
	}

	ans := q.Answers[1]
	wCorrect, wScore, wIndex := true, ans.Score, 1
	texts := []string{
		ans.Text[0],
		ans.Text[0] + " ",
		" " + ans.Text[0] + " ",
		strings.ToUpper(ans.Text[0]),
	}
	for _, text := range texts {
		gCorrect, gScore, gIndex := q.checkAnswer(text)

		if gCorrect != wCorrect || gScore != wScore {
			t.Errorf("want: correct %t got %t, score %d got %d, index %d got %d", wCorrect, gCorrect, wScore, gScore, wIndex, gIndex)
		}
	}
}

func TestNextQuestion(t *testing.T) {
	seed, played := int64(0), 0
	_, err := NextQuestion(seed, played, 10)
	if err != nil {
		t.Error(err)
	}
}
