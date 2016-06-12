package fam100

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/boltdb/bolt"
)

var (
	// DB question database
	QuestionDB     *bolt.DB
	questionSize   int
	questionBucket = []byte("questions")
)

// Question for a round
type Question struct {
	ID      int
	Text    string
	Answers []answer
}

type answer struct {
	ID    int
	Text  []string
	Score int
}

func InitQuestion(dbPath string) (numQuestion int, err error) {
	QuestionDB, err = bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return 0, err
	}
	err = QuestionDB.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(questionBucket)
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	err = QuestionDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(questionBucket)
		stats := b.Stats()
		questionSize = stats.KeyN
		return nil
	})
	if err != nil {
		return 0, err
	}

	if questionSize == 0 {
		return 0, fmt.Errorf("Loaded 0 questions")
	}

	return questionSize, nil
}

func (a answer) String() string {
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

func AddQuestion(q Question) error {
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	if err := enc.Encode(q); err != nil {
		return err
	}

	QuestionDB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(questionBucket)
		id := strconv.FormatInt(int64(q.ID), 10)
		err := b.Put([]byte(id), buff.Bytes())
		return err
	})
	return nil
}

func GetQuestion(id string) (q Question, err error) {
	err = QuestionDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(questionBucket)
		v := b.Get([]byte(id))

		buff := bytes.NewBuffer(v)
		dec := gob.NewDecoder(buff)
		if err := dec.Decode(&q); err != nil {
			return err
		}

		return nil
	})
	return q, err
}

// check answers gives the score for particular answer to a question
func (q Question) checkAnswer(text string) (correct bool, score, index int) {
	text = strings.TrimSpace(strings.ToLower(text))

	for i, ans := range q.Answers {
		for _, ansText := range ans.Text {
			if text == ansText {
				return true, ans.Score, i
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
	id := order[played%questionSize] + 1 // order is 0 based
	idStr := strconv.FormatInt(int64(id), 10)

	return GetQuestion(idStr)
}
