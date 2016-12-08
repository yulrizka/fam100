package qa

import (
	"database/sql"
	"net/url"

	// use mysql as sql database
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
)

// Mysql persistence provider
//
// create table SQL
//
// CREATE TABLE IF NOT EXISTS questions (
// 	`id` int NOT NULL AUTO_INCREMENT PRIMARY KEY,
// 	`text` varchar(256) NOT NULL,
// 	`answers` TEXT NOT NULL,
// 	`status` varchar(20),
// 	`active` BOOLEAN
// );
type Mysql struct {
	db             *sql.DB
	activeQuestion int
}

// NewMysql creates new instance of mysql. It will fail if it can not connect to DB
func NewMysql(dsn string) (*Mysql, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, errors.Wrap(err, "invalid DSN")
	}
	u.Query().Add("parseTime", "true")

	db, err := sql.Open("mysql", u.String())
	if err != nil {
		return nil, err
	}

	var count int
	err = db.QueryRow("SELECT COUNT(id) from questions WHERE status = 'active'").Scan(&count)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting count from db")
	}

	return &Mysql{db: db, activeQuestion: count}, nil
}

// Close Mysql connection
func (m *Mysql) Close() error {
	return m.db.Close()
}

func (m *Mysql) Add(q *Question) error {
	result, err := m.db.Exec("INSERT INTO questions (`text`, answers, status, active) VALUES (?,?,?,?)",
		q.Text, q.formatAnswers(), string(q.Status), q.Active)
	if err != nil {
		return errors.Wrap(err, "adding question failed")
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	q.ID = int(id)

	return nil
}

func (m *Mysql) Get(id int) (*Question, error) {
	var q Question

	var text, answers string
	var status string
	var active bool

	err := m.db.QueryRow("SELECT `text`, answers, status, active from questions WHERE ID = ?", id).Scan(
		&text, &answers, &status, &active,
	)
	switch {
	case err == sql.ErrNoRows:
		return nil, nil
	case err != nil:
		return nil, errors.Wrapf(err, "failed getting question %s", id)
	}

	q.ID = id
	q.Text = text
	q.Status = parseStatus(status)
	q.Active = active
	q.Answers = parseAnswer(answers)

	return &q, nil
}

func (m *Mysql) Update(q *Question) error {
	panic("not implemented")
}

func (m *Mysql) Delete(id int) error {
	_, err := m.db.Exec("DELETE from questions where id = ?", id)
	return err
}

func (m *Mysql) Next(seed int64, played int, questionLimit int) (Question, error) {
	panic("not implemented")
}

func (m *Mysql) Count() (int, error) {
	return m.activeQuestion, nil
}
