package fam100

import (
	"math/rand"
	"sort"
	"time"

	"github.com/yulrizka/fam100/model"
	"github.com/yulrizka/fam100/qna"
)

// round represents with one question
type round struct {
	id        int64
	q         qna.Question
	state     State
	correct   []model.PlayerID // correct answer answered by a player, "" means not answered
	players   map[model.PlayerID]model.Player
	highlight map[int]bool

	endAt time.Time
}

func newRound(question qna.Question, players map[model.PlayerID]model.Player) (*round, error) {
	return &round{
		id:        int64(rand.Int31()),
		q:         question,
		correct:   make([]model.PlayerID, len(question.Answers)),
		state:     Created,
		players:   players,
		highlight: make(map[int]bool),
		endAt:     time.Now().Add(RoundDuration).Round(time.Second),
	}, nil
}

func (r *round) timeLeft() time.Duration {
	return r.endAt.Sub(time.Now().Round(time.Second))
}

// questionText construct QNAMessage which contains questions, answers and score
func (r *round) questionText(gameID string, showUnAnswered bool) QNAMessage {
	ras := make([]roundAnswers, len(r.q.Answers))

	for i, ans := range r.q.Answers {
		ra := roundAnswers{
			Text:  ans.String(),
			Score: ans.Score,
		}
		if pID := r.correct[i]; pID != "" {
			ra.Answered = true
			ra.PlayerName = r.players[pID].Name
		}
		if r.highlight[i] {
			ra.Highlight = true
		}
		ras[i] = ra
	}

	msg := QNAMessage{
		ChanID:         gameID,
		QuestionText:   r.q.Text,
		QuestionID:     r.q.ID,
		ShowUnanswered: showUnAnswered,
		TimeLeft:       r.timeLeft(),
		Answers:        ras,
	}

	return msg
}

func (r *round) finished() bool {
	answered := 0
	for _, pID := range r.correct {
		if pID != "" {
			answered++
		}
	}

	return answered == len(r.q.Answers)
}

// ranking generates a rank for current round which contains player, answers and score
func (r *round) ranking() model.Rank {
	var roundScores model.Rank
	lookup := make(map[model.PlayerID]model.PlayerScore)
	for i, pID := range r.correct {
		if pID != "" {
			score := r.q.Answers[i].Score
			if ps, ok := lookup[pID]; !ok {
				lookup[pID] = model.PlayerScore{
					PlayerID: pID,
					Name:     r.players[pID].Name,
					Score:    score,
				}
			} else {
				ps = lookup[pID]
				ps.Score += score
				lookup[pID] = ps
			}
		}
	}

	for _, ps := range lookup {
		roundScores = append(roundScores, ps)
	}
	sort.Sort(roundScores)
	for i := range roundScores {
		roundScores[i].Position = i + 1
	}

	return roundScores
}

func (r *round) answer(p model.Player, text string) (correct, answered bool, index int) {
	if r.state != RoundStarted {
		return false, false, -1
	}

	if _, ok := r.players[p.ID]; !ok {
		r.players[p.ID] = p
	}
	if correct, _, i := r.q.CheckAnswer(text); correct {
		if r.correct[i] != "" {
			// already answered
			return correct, true, i
		}
		r.correct[i] = p.ID
		r.highlight[i] = true

		return correct, false, i
	}
	return false, false, -1
}
