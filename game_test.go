package fam100

import (
	"math/rand"
	"testing"

	"github.com/yulrizka/fam100/model"
	"github.com/yulrizka/fam100/qna"
	"github.com/yulrizka/fam100/repo"
)

func Test_Game(t *testing.T) {
	repo.RedisPrefix = "test_fam100"
	if err := repo.DefaultDB.Init(); err != nil {
		t.Fatalf("failed to load database: %v", err)
	}

	if err := repo.DefaultDB.Reset(); err != nil {
		t.Fatalf("failed to reset database: %v", err)
	}

	questionDB, err := qna.NewText("qna/famili100.txt")
	if err != nil {
		t.Fatalf("failed to load questions: %v", err)
	}

	t.Run("test questionString", func(t *testing.T) {
		var seed, totalRoundPlayed = 7, 0

		players := map[model.PlayerID]model.Player{
			"1": {ID: "1", Name: "foo"},
			"2": {ID: "2", Name: "bar"},
			"3": {ID: "3", Name: "baz"},
		}

		q, err := questionDB.NextQuestion(int64(seed), totalRoundPlayed, 10)
		if err != nil {
			t.Fatalf("failed to get next questions: %v", err)
		}

		r, err := newRound(q, players)
		if err != nil {
			t.Error(err)
		}
		r.state = Started
		rand.Seed(7)
		idx := rand.Perm(len(r.q.Answers))
		for i := 0; i < len(players); i++ {
			answerText := r.q.Answers[idx[i]].Text[0]
			// get random player
			var player model.Player
			for _, value := range players {
				player = value
				break
			}
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
	})
}
