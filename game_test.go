package fam100

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestQuestionString(t *testing.T) {
	q, err := GetQuestion(17)
	if err != nil {
		t.Error(err)
	}
	g, _ := NewRound(q, nil)
	if err != nil {
		t.Error(err)
	}
	g.Start()

	rand.Seed(7)
	players := []Player{
		Player{ID: "1", Name: "foo"},
		Player{ID: "2", Name: "bar"},
		Player{ID: "3", Name: "baz"},
	}
	idx := rand.Perm(len(q.answers))
	for i := 0; i < len(players); i++ {
		answerText := q.answers[idx[i]].text[0]
		player := players[rand.Intn(len(players))]
		g.answer(player, answerText)
	}
	// no checking, just to debug output
	showUnAnswered := true
	fmt.Print(g.questionText(showUnAnswered))
	fmt.Println()
	for pID, score := range g.scores() {
		p := g.players[pID]
		fmt.Printf("p.name = %s, score = %d\n", p.Name, score)
	}
}
