package qna

import (
	"bytes"
	"strings"
)

var (
	// ExtraQuestionSeed seed the random for question
	ExtraQuestionSeed = int64(0)
)

// Provider provides persistence functionality for question and answers
type Provider interface {
	// AddQuestion new to the DB
	AddQuestion(q Question) error

	// GetQuestion by id
	GetQuestion(id string) (Question, error)

	// NextQuestion generates next question randomly by taking into account
	// numbers of game played for particular seed key
	NextQuestion(seed int64, played int, questionLimit int) (Question, error)

	// Count total active question
	Count() (int, error)
}

// Question for a round
type Question struct {
	ID      int
	Text    string
	Answers []Answer
	lookup  map[string]int
}

// Check answers gives the score for particular answer to a question
func (q Question) CheckAnswer(text string) (correct bool, score, index int) {
	text = strings.TrimSpace(strings.ToLower(text))
	if i, ok := q.lookup[text]; ok {
		return true, q.Answers[i].Score, i
	}

	return false, 0, -1
}

// Answer to a Question
type Answer struct {
	ID    int
	Text  []string
	Score int
}

func (a Answer) String() string {
	if len(a.Text) == 1 {
		return a.Text[0]
	}

	var b bytes.Buffer
	for i, text := range a.Text {
		if i != 0 {
			b.WriteString(" / ")
		}
		b.WriteString(text)
	}
	return b.String()
}
