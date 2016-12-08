package qa

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type Status string

const (
	Pending  Status = "pending"
	Accepted Status = "accepted"
	Rejected Status = "rejected"
)

func parseStatus(s string) Status {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "pending", "accepted", "rejected":
		return Status(s)
	default:
		return ""
	}
}

var (
	// ExtraQuestionSeed seed the random for question
	ExtraQuestionSeed = int64(0)
)

// Provider provides persistence functionalities for question and answers
type Provider interface {
	// Add new to the DB
	Add(*Question) error

	// Get by id
	Get(id int) (*Question, error)

	// Updatec
	Update(*Question) error

	// Delete
	Delete(id int) error

	// Next generates next question randomly by taking into account
	// numbers of game played for particular seed key
	Next(seed int64, played int, questionLimit int) (Question, error)

	// Count total active question
	Count() (int, error)

	// Close the database
	Close() error
}

// Question for a round
type Question struct {
	ID      int
	Text    string
	Answers []Answer
	Status  Status
	Active  bool

	lookup map[string]int
}

func NewQuestion(text string, answers []Answer, status Status, active bool) Question {
	q := Question{
		Text:    strings.TrimSpace(strings.ToLower(text)),
		Status:  status,
		Active:  active,
		Answers: make([]Answer, 0, len(answers)),
	}

	for _, a := range answers {
		ans := Answer{Score: a.Score, Text: make([]string, len(a.Text))}
		for i, txt := range a.Text {
			ans.Text[i] = strings.TrimSpace(strings.ToLower(txt))
		}
		q.Answers = append(q.Answers, ans)
	}

	return q
}

// check answers gives the score for particular answer to a question
func (q Question) Check(text string) (correct bool, score, index int) {
	text = strings.TrimSpace(strings.ToLower(text))
	if i, ok := q.lookup[text]; ok {
		return true, q.Answers[i].Score, i
	}

	return false, 0, -1
}

func (q Question) Format() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d;", q.ID)
	fmt.Fprintf(&buf, "%s;", q.Text)
	fmt.Fprintf(&buf, "%s;", q.formatAnswers())
	fmt.Fprintf(&buf, "%s;", q.Status)
	fmt.Fprintf(&buf, "%t", q.Active)

	return buf.String()
}

func (q Question) formatAnswers() string {
	var buf bytes.Buffer
	for i, ans := range q.Answers {
		if i > 0 {
			fmt.Fprintf(&buf, "*")
		}
		fmt.Fprintf(&buf, "%d:%s", ans.Score, ans.Format())
	}
	return buf.String()
}

func Parse(s string) (Question, error) {
	var q Question
	s = strings.TrimSpace(s)
	if s == "" {
		return q, nil
	}
	s = strings.ToLower(s)

	fields := strings.SplitN(s, ";", 5)
	if len(fields) == 0 {
		return q, nil
	}

	switch {
	case len(fields) > 0:
		id, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return q, fmt.Errorf("failed parsing id %q", fields[0])
		}
		q.ID = int(id)
		fallthrough
	case len(fields) > 1:
		q.Text = strings.TrimSpace(strings.ToLower(fields[1]))
		fallthrough
	case len(fields) > 2:
		q.Answers = parseAnswer(fields[2])
		fallthrough
	case len(fields) > 3:
		q.Status = parseStatus(fields[3])
		fallthrough
	case len(fields) > 4:
		q.Active = strings.TrimSpace(fields[4]) == "true"
	}

	return q, nil
}

func parseAnswer(s string) []Answer {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return nil
	}

	var ans []Answer
	fields := strings.Split(s, "*")
	if len(fields) == 0 {
		return ans
	}

	for _, rawAns := range fields {
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
		alias := strings.Split(rawAns, "/")
		for _, answer := range alias {
			text := answer
			a.Text = append(a.Text, text)
		}
		ans = append(ans, a)
	}

	return ans
}

// Answer to a Qeustion
type Answer struct {
	Text  []string
	Score int
}

func (a Answer) Format() string {
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
