package fam100

import (
	"log"
	"time"
)

var (
	roundDuration        = 60 * time.Second
	tickDuration         = 10 * time.Second
	delayBetweenRound    = 5 * time.Second
	TickAfterWrongAnswer = false
	RoundPerGame         = 3
)

// Message to communitace between player and the game
type Message interface{}

// TextMessage represents a chat message
type TextMessage struct {
	ChanID string
	Player Player
	Text   string
}

// StateMessage represents state change in the game
type StateMessage struct {
	ChanID    string
	Round     int
	State     State
	RoundText RoundTextMessage
}

// TickMessage represents time left notification
type TickMessage struct {
	ChanID   string
	TimeLeft time.Duration
}

// RoundTextMessage represents question and answer for this round
type RoundTextMessage struct {
	ChanID         string
	Round          int
	QuestionText   string
	QuestionID     int
	Answers        []roundAnswers
	ShowUnanswered bool // reveal un-answered question
	TimeLeft       time.Duration
}

type roundAnswers struct {
	Text       string
	Score      int
	Answered   bool
	PlayerName string
}

// PlayerID is the player ID type
type PlayerID string

// Player of the game
type Player struct {
	ID   PlayerID
	Name string
}

// State represents state of the round
type State string

// RoundState Kind
const (
	Created       State = "created"
	Started       State = "started"
	Finished      State = "finished"
	RoundStarted  State = "roundStarted"
	RoundTimeout  State = "RoundTimeout"
	RoundFinished State = "roundFinished"
)

// Game can consist of multiple round
// each round user will be asked question and gain ponint
type Game struct {
	ChanID           string
	State            State
	TotalRoundPlayed int
	players          map[PlayerID]Player
	seed             int64

	In  chan Message
	Out chan Message
}

// NewGame create a new round
// Seed and totalRoundPlayed determine the random order of question
// Seed can be any number, for example unix timestamp
func NewGame(id string, in, out chan Message) (r *Game, err error) {
	seed, totalRoundPlayed, err := DefaultDB.nextGame(id)
	if err != nil {
		return nil, err
	}
	log.Printf("Started game channel:%s, seed:%d, totalRoundPlayed:%d", id, seed, totalRoundPlayed)

	return &Game{
		ChanID:           id,
		State:            Created,
		players:          make(map[PlayerID]Player),
		seed:             seed,
		TotalRoundPlayed: totalRoundPlayed,
		In:               in,
		Out:              out,
	}, err
}

// Start the game
func (g *Game) Start() {
	g.State = Started
	go func() {
		g.Out <- StateMessage{ChanID: g.ChanID, State: Started}
		for i := 0; i < RoundPerGame; i++ {
			g.startRound(i + 1)
			if i != RoundPerGame-1 {
				time.Sleep(delayBetweenRound)
			}
		}
		g.Out <- StateMessage{ChanID: g.ChanID, State: Finished}
	}()
}

func (g *Game) startRound(currentRound int) error {
	g.TotalRoundPlayed++
	DefaultDB.incRoundPlayed(g.ChanID)
	r, err := newRound(g.seed, g.TotalRoundPlayed, g.players)
	if err != nil {
		return err
	}
	r.state = RoundStarted
	timeout := time.After(roundDuration)
	timeLeftTick := time.NewTicker(tickDuration)

	// print question
	g.Out <- StateMessage{
		ChanID:    g.ChanID,
		State:     RoundStarted,
		Round:     currentRound,
		RoundText: r.questionText(g.ChanID, false),
	}

	for {
		select {
		case rawMsg := <-g.In: // new answer coming from player
			msg, ok := rawMsg.(TextMessage)
			if !ok {
				log.Printf("ERROR Unexpected message type input from client")
				continue
			}
			answer := msg.Text
			correct, _, _ := r.answer(msg.Player, answer)
			if !correct {
				if TickAfterWrongAnswer {
					g.Out <- TickMessage{ChanID: g.ChanID, TimeLeft: r.timeLeft()}
				}
				continue
			}

			// show correct answer
			g.Out <- r.questionText(g.ChanID, false)
			if r.finised() {
				r.state = RoundFinished
				timeLeftTick.Stop()
				g.Out <- StateMessage{ChanID: g.ChanID, State: RoundFinished, Round: currentRound}
				return nil
			}
		case <-timeLeftTick.C: // inform time left
			select {
			case g.Out <- TickMessage{ChanID: g.ChanID, TimeLeft: r.timeLeft()}:
			default:
			}
		case <-timeout:
			g.State = RoundFinished
			timeLeftTick.Stop()
			g.Out <- StateMessage{ChanID: g.ChanID, State: RoundTimeout, Round: currentRound}
			showUnAnswered := true
			msg := r.questionText(g.ChanID, showUnAnswered)
			g.Out <- msg
			return nil
		}
	}
}

// round represents one quesiton round
type round struct {
	q       Question
	state   State
	correct []PlayerID // correct answer answered by a player, "" means not answered
	players map[PlayerID]Player
	endAt   time.Time
}

func newRound(seed int64, totalRoundPlayed int, players map[PlayerID]Player) (*round, error) {
	q, err := NextQuestion(seed, totalRoundPlayed)
	if err != nil {
		return nil, err
	}

	return &round{
		q:       q,
		correct: make([]PlayerID, len(q.answers)),
		state:   Created,
		players: players,
		endAt:   time.Now().Add(roundDuration).Round(time.Second),
	}, nil
}

func (r *round) timeLeft() time.Duration {
	return r.endAt.Sub(time.Now().Round(time.Second))
}

func (r *round) questionText(gameID string, showUnAnswered bool) RoundTextMessage {
	ras := make([]roundAnswers, len(r.q.answers))

	for i, ans := range r.q.answers {
		ra := roundAnswers{
			Text:  ans.String(),
			Score: ans.score,
		}
		if pID := r.correct[i]; pID != "" {
			ra.Answered = true
			ra.PlayerName = r.players[pID].Name
		}
		ras[i] = ra
	}

	msg := RoundTextMessage{
		ChanID:         gameID,
		QuestionText:   r.q.text,
		QuestionID:     r.q.id,
		ShowUnanswered: showUnAnswered,
		TimeLeft:       r.timeLeft(),
		Answers:        ras,
	}

	return msg
}

func (r *round) finised() bool {
	answered := 0
	for _, pID := range r.correct {
		if pID != "" {
			answered++
		}
	}

	return answered == len(r.q.answers)
}

func (r *round) scores() map[PlayerID]int {
	scores := make(map[PlayerID]int)
	for i, pID := range r.correct {
		if pID != "" {
			scores[pID] = r.q.answers[i].score
		}
	}
	return scores
}

func (r *round) answer(p Player, text string) (correct, answered bool, index int) {
	if r.state != RoundStarted {
		return false, false, -1
	}

	if correct, _, i := r.q.checkAnswer(text); correct {
		if r.correct[i] != "" {
			// already answered
			return correct, true, i
		}
		r.correct[i] = p.ID
		r.players[p.ID] = p

		return correct, false, i
	}
	return false, false, -1
}
