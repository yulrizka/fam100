package qa

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
	questionBucket = []byte("questions")
)

// Bolt DB backed Question and Answer storage
type Bolt struct {
	db           *bolt.DB
	questionSize int
}

// NewBolt create and initialize bolt database for the given path
func NewBolt(dbPath string) (*Bolt, error) {
	var err error
	d := Bolt{}
	d.db, err = bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, err
	}
	err = d.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(questionBucket)
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(questionBucket)
		stats := b.Stats()
		d.questionSize = stats.KeyN
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &d, nil
}

// Close database
func (d *Bolt) Close() error {
	return d.db.Close()
}

// AddQuestion see Provider
func (d *Bolt) AddQuestion(q Question) error {
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	if err := enc.Encode(q); err != nil {
		return err
	}

	d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(questionBucket)
		id := strconv.FormatInt(int64(q.ID), 10)
		err := b.Put([]byte(id), buff.Bytes())
		return err
	})

	return nil
}

// Count see Provider
func (d *Bolt) Count() (int, error) {
	return d.questionSize, nil
}

// GetQuestion see Provider
func (d *Bolt) GetQuestion(id string) (q Question, err error) {
	err = d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(questionBucket)
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

// NextQuestion generates next question randomly by taking into account
// numbers of game played for particular seed key
func (d *Bolt) NextQuestion(seed int64, played int, questionLimit int) (q Question, err error) {
	questionSize, err := d.Count()
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

	return d.GetQuestion(idStr)
}
