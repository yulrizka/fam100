package main

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestQuestionString(t *testing.T) {
	q, err := getQuestion(17)
	if err != nil {
		t.Error(err)
	}
	g, err := newRound(q)
	if err != nil {
		t.Error(err)
	}
	g.start()

	rand.Seed(7)
	players := []player{
		player{id: "1", name: "foo"},
		player{id: "2", name: "bar"},
		player{id: "3", name: "baz"},
	}
	idx := rand.Perm(len(q.answers))
	for i := 0; i < len(players); i++ {
		answerText := q.answers[idx[i]].text[0]
		player := players[rand.Intn(len(players))]
		g.answer(player, answerText)
	}
	// no checking, just to debug output
	fmt.Print(g.questionText())
	fmt.Println()
	for pID, score := range g.scores() {
		p := g.players[pID]
		fmt.Printf("p.name = %s, score = %d\n", p.name, score)
	}
}
