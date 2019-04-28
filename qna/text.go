package qna

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type Text struct {
	questions    []Question
	questionsMap map[string]Question
}

func NewText(path string) (*Text, error) {
	t := Text{
		questions:    make([]Question, 0),
		questionsMap: make(map[string]Question),
	}

	// load question from the text
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %q", path)
	}
	s := bufio.NewScanner(f)
	i := 0
	for s.Scan() {
		i++
		q, ok := scanQuestionRaw(s.Text())
		if !ok {
			continue
		}
		q.ID = i
		if err := t.AddQuestion(q); err != nil {
			return nil, errors.Wrapf(err, "failed adding question at line %d", i)
		}
	}

	return &t, nil
}

func (t *Text) AddQuestion(q Question) error {
	t.questions = append(t.questions, q)
	t.questionsMap[strconv.FormatInt(int64(q.ID), 10)] = q

	return nil
}

func (t *Text) GetQuestion(id string) (q Question, err error) {
	q, ok := t.questionsMap[id]
	if !ok {
		return Question{}, fmt.Errorf("question with id %s not found", id)
	}
	return q, nil
}

func (t *Text) NextQuestion(seed int64, played int, questionLimit int) (q Question, err error) {
	questionSize, err := t.Count()
	if err != nil {
		return q, err
	}
	if questionLimit <= 0 || questionLimit > questionSize {
		questionLimit = questionSize
	}

	r := rand.New(rand.NewSource(seed + ExtraQuestionSeed))
	order := r.Perm(questionSize)
	id := order[played%questionLimit] + 1 // order is 0 based
	idStr := strconv.FormatInt(int64(id), 10)

	return t.GetQuestion(idStr)
}

func (t *Text) Count() (int, error) {
	return len(t.questions), nil
}

func scanQuestionRaw(s string) (Question, bool) {
	var q Question

	// apa yang berhubungan dengan tarzan*30:hutan*21:hewan*16:teriakan auoo / auoo*12:jane*7:bergelantungan*3:tali / akar*
	fields := strings.Split(s, "*")
	if len(fields) == 0 {
		return q, false
	}
	q.lookup = make(map[string]int)
	s = strings.ToLower(s)
	q.Text = fields[0]  // question "apa yang berhubungan dengan tarzan"
	fields = fields[1:] // [30:hutan, 21:hewan, 16:teriakan auoo / auoo, 12:jane, 7:bergelantungan, 3:tali / akar*]

	for ansIndex, rawAns := range fields {
		rawAns = strings.TrimSpace(rawAns)
		var a Answer
		if i := strings.Index(rawAns, ":"); i > 0 {
			if score, err := strconv.ParseInt(rawAns[:i], 10, 64); err == nil {
				a.Score = int(score)
			}
			rawAns = rawAns[i+1:]
		}

		if rawAns == "" {
			continue
		}

		// teriakan auoo / auoo
		alias := strings.Split(rawAns, "/")
		for _, answer := range alias {
			text := strings.TrimSpace(string(answer))
			a.Text = append(a.Text, text)
			q.lookup[text] = ansIndex
		}
		q.Answers = append(q.Answers, a)
	}

	return q, true
}
