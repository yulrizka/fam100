package fam100

import (
	"math/rand"
	"os"
	"testing"

	"github.com/yulrizka/fam100/model"
	"github.com/yulrizka/fam100/repo"
)

func TestMain(m *testing.M) {
	repo.RedisPrefix = "test_fam100"
	if _, err := InitQuestion("test.db"); err != nil {
		panic(err)
	}
	repo.DefaultDB.Init()
	repo.DefaultDB.Reset()
	retCode := m.Run()
	os.Exit(retCode)
}

func TestQuestionString(t *testing.T) {
	var seed, totalRoundPlayed = 7, 0
	r, err := newRound(int64(seed), totalRoundPlayed, make(map[model.PlayerID]model.Player), 10)
	if err != nil {
		t.Error(err)
	}
	r.state = Started
	rand.Seed(7)
	players := []model.Player{
		model.Player{ID: "1", Name: "foo"},
		model.Player{ID: "2", Name: "bar"},
		model.Player{ID: "3", Name: "baz"},
	}
	idx := rand.Perm(len(r.q.Answers))
	for i := 0; i < len(players); i++ {
		answerText := r.q.Answers[idx[i]].Text[0]
		player := players[rand.Intn(len(players))]
		r.answer(player, answerText)
	}
	// no checking, just to debug output
	/*
		showUnAnswered := false
		fmt.Print(r.questionText(showUnAnswered))
		fmt.Println()
		for pID, score := range r.scores() {
			p := r.players[pID]
			fmt.Printf("p.name = %s, score = %d\n", p.Name, score)
		}
	*/
}
