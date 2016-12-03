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

// Database configuration
var (
	DefaultQuestionDB QuestionDB
	QuestionBucket    = []byte("questions")

	// ExtraQuestionSeed seed the random for question
	ExtraQuestionSeed = int64(0)
)

func InitQuestion(dbPath string) (numQuestion int, err error) {
	if err := DefaultQuestionDB.Initialize(dbPath); err != nil {
		return 0, err
	}

	questionSize := DefaultQuestionDB.questionSize
	DefaultQuestionLimit = int(float64(questionSize) * 0.8)

	return questionSize, nil
}

type QuestionDB struct {
	DB           *bolt.DB
	questionSize int
}

func (d *QuestionDB) Initialize(dbPath string) error {
	var err error
	d.DB, err = bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return err
	}
	err = d.DB.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(QuestionBucket)
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = d.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(QuestionBucket)
		stats := b.Stats()
		d.questionSize = stats.KeyN
		return nil
	})
	if err != nil {
		return err
	}

	if d.questionSize == 0 {
		return fmt.Errorf("Loaded 0 questions")
	}

	return nil
}

func (d *QuestionDB) Close() error {
	return d.DB.Close()
}

func (d *QuestionDB) AddQuestion(q Question) error {
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	if err := enc.Encode(q); err != nil {
		return err
	}

	d.DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(QuestionBucket)
		id := strconv.FormatInt(int64(q.ID), 10)
		err := b.Put([]byte(id), buff.Bytes())
		return err
	})

	return nil
}

func AddQuestion(q Question) error {
	return DefaultQuestionDB.AddQuestion(q)
}

func (d *QuestionDB) GetQuestion(id string) (q Question, err error) {
	err = d.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(QuestionBucket)
		v := b.Get([]byte(id))

		buff := bytes.NewBuffer(v)
		dec := gob.NewDecoder(buff)
		if err := dec.Decode(&q); err != nil {
			return err
		}

		q.lookup = make(map[string]int)
		for i, ans := range q.Answers {
			for _, text := range ans.Text {
				text = strings.TrimSpace(strings.ToLower(text))
				q.lookup[text] = i
			}
		}

		return nil
	})
	return q, err
}
func GetQuestion(id string) (q Question, err error) {
	return DefaultQuestionDB.GetQuestion(id)
}

// NextQuestion generates next question randomly by taking into account
// numbers of game played for particular seed key
func NextQuestion(seed int64, played int, questionLimit int) (q Question, err error) {
	questionSize := DefaultQuestionDB.questionSize
	if questionLimit <= 0 || questionLimit > questionSize {
		questionLimit = questionSize
	}
	r := rand.New(rand.NewSource(seed + ExtraQuestionSeed))
	order := r.Perm(questionSize)
	id := order[played%questionLimit] + 1 // order is 0 based
	idStr := strconv.FormatInt(int64(id), 10)

	return GetQuestion(idStr)
}

// Question for a round
type Question struct {
	ID      int
	Text    string
	Answers []Answer
	lookup  map[string]int
}

// check answers gives the score for particular answer to a question
func (q Question) checkAnswer(text string) (correct bool, score, index int) {
	text = strings.TrimSpace(strings.ToLower(text))
	if i, ok := q.lookup[text]; ok {
		return true, q.Answers[i].Score, i
	}

	return false, 0, -1
}

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
