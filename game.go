package main

import (
	"bufio"
	"bytes"
	"fmt"
)

type playerID string
type player struct {
	id   playerID
	name string
}

type roundState string

const (
	created  roundState = "created"
	started  roundState = "started"
	finished roundState = "finished"
)

type round struct {
	q       question
	correct []playerID // correct answer answered by a player, nil means not answered
	state   roundState
	players map[playerID]player
}

func newRound(q question) (*round, error) {
	return &round{
		q:       q,
		correct: make([]playerID, len(q.answers)),
		state:   created,
		players: make(map[playerID]player),
	}, nil
}

func (g *round) start() {
	g.state = started
}

func (g *round) stop() {
	g.state = finished
}

func (g *round) player(id playerID) (player, bool) {
	p, ok := g.players[id]
	return p, ok
}

func (g *round) questionText() string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[%d] %s?\n\n", g.q.id, g.q.text)
	for i, a := range g.q.answers {
		if pID := g.correct[i]; pID != "" {
			fmt.Fprintf(w, "%d. %-30s [%d] - %s\n", i+1, a.String(), a.score, g.players[pID].name)
		} else {
			fmt.Fprintf(w, "%d. ______________________________\n", i+1)
		}
	}
	w.Flush()

	return b.String()
}

func (g *round) scores() map[playerID]int {
	scores := make(map[playerID]int)
	for i, pID := range g.correct {
		if pID != "" {
			scores[pID] = g.q.answers[i].score
		}
	}
	return scores
}

func (g *round) answer(p player, text string) (correct bool, score, index int) {
	if g.state != started {
		return false, 0, -1
	}

	if correct, score, i := g.q.checkAnswer(text); correct {
		g.correct[i] = p.id
		g.players[p.id] = p
		return correct, score, i
	}
	return false, 0, -1
}
