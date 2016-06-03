package fam100

import (
	"bytes"
	"database/sql"
	"fmt"
	"math/rand"
	"strings"

	_ "github.com/mattn/go-sqlite3" // sqlite3 requirement
)

var (
	db           *sql.DB
	questionSize int
)

// Question for a round
type Question struct {
	id            int
	text          string
	answers       []answer
	answersLookup map[string]*answer
}

type answer struct {
	id    int
	text  []string
	score int
}

func (a answer) String() string {
	if len(a.text) == 1 {
		return a.text[0]
	}

	var b bytes.Buffer
	for i, text := range a.text {
		if i != 0 {
			b.WriteString(" / ")
		}
		b.WriteString(text)
	}
	return b.String()
}

// LoadQuestion db
func LoadQuestion(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}

	rows, err := db.Query(`SELECT count(id_soal) from soal`)
	if err != nil {
		return err
	}
	if !rows.Next() {
		return fmt.Errorf("failed get size of question")
	}
	if err := rows.Scan(&questionSize); err != nil {
		return err
	}

	return nil
}

// GetQuestion by id
func GetQuestion(id int) (q Question, err error) {
	rows, err := db.Query(`select soal.id_soal, soal, id_jawaban, jawaban, jawaban_alt, skor 
		FROM soal inner join jawaban on soal.id_soal = jawaban.id_soal
		WHERE soal.id_soal = ? ORDER BY skor DESC
	`, id)
	if err != nil {
		return q, err
	}
	defer rows.Close()
	q.answersLookup = make(map[string]*answer)

	for rows.Next() {
		var a answer
		aText := make([]sql.NullString, 2, 2)
		if err := rows.Scan(&q.id, &q.text, &a.id, &aText[0], &aText[1], &a.score); err != nil {
			return q, err
		}
		for _, text := range aText {
			if text.String != "" {
				a.text = append(a.text, text.String)
				q.answersLookup[text.String] = &a
			}
		}
		if len(a.text) == 0 {
			return q, fmt.Errorf("got empty text in one of the answers, question id %d", id)
		}
		q.answers = append(q.answers, a)
	}

	return q, err
}

// check answers gives the score for particular answer to a question
func (q Question) checkAnswer(text string) (correct bool, score, index int) {
	text = strings.TrimSpace(strings.ToLower(text))

	for i, ans := range q.answers {
		for _, ansText := range ans.text {
			if text == ansText {
				return true, ans.score, i
			}
		}
	}

	return false, 0, -1
}

// NextQuestion generates next question randomly by taking into account
// numbers of game played for particular seed key
func NextQuestion(seed int64, played int) (q Question, err error) {
	r := rand.New(rand.NewSource(seed))
	order := r.Perm(questionSize)
	questionID := order[played%questionSize]

	return GetQuestion(questionID)
}
