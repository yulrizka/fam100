package fam100

import (
	"bufio"
	"bytes"
	"fmt"
	"time"
)

var (
	roundDuration = 60 * time.Second
	tickDuration  = 10 * time.Second
)

// Message to communitace between player and the game
type Message struct {
	GameID string
	Kind   string
	Player Player
	Text   string
}

// Kind of Message
const (
	TextMessage  = "textMsg"
	StateMessage = "state"
	TickMessage  = "tick"
)

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
	RoundFinished State = "roundFinished"
)

// Game can consist of multiple round
// each round user will be asked question and gain ponint
type Game struct {
	ID          string
	State       State
	players     map[PlayerID]Player
	seed        int64
	roundPlayed int

	In  chan Message
	Out chan Message
}

// NewGame create a new round
// Seed and roundPlayed determine the random order of question
// Seed can be any number, for example unix timestamp
func NewGame(id string, seed int64, roundPlayed int, in, out chan Message) (r *Game) {
	r = &Game{
		ID:          id,
		State:       Created,
		players:     make(map[PlayerID]Player),
		seed:        seed,
		roundPlayed: roundPlayed,
		In:          in,
		Out:         out,
	}
	return r
}

// Start the game
func (g *Game) Start() {
	g.State = Started
	go func() {
		nRound := 3
		for i := 0; i < nRound; i++ {
			text := fmt.Sprintf(T("Ronde %d dari %d"), i+1, nRound)
			g.Out <- Message{GameID: g.ID, Kind: TextMessage, Text: text}
			g.startRound()
			g.roundPlayed++
		}
		g.Out <- Message{GameID: g.ID, Kind: StateMessage, Text: string(Finished)}
	}()
}

func (g *Game) startRound() error {
	g.roundPlayed++
	r, err := newRound(g.seed, g.roundPlayed, g.players)
	if err != nil {
		return err
	}
	r.state = Started
	timeout := time.After(roundDuration)
	timeLeftTick := time.NewTicker(tickDuration)

	// print question
	g.Out <- Message{GameID: g.ID, Kind: StateMessage, Text: string(Started)}

	qText := r.questionText(false)
	qText += "\n"
	qText += fmt.Sprintf(T("Anda memiliki waktu %s"), roundDuration)
	g.Out <- Message{GameID: g.ID, Kind: TextMessage, Text: qText}

	for {
		select {
		case msg := <-g.In: // new answer coming from player
			answer := msg.Text
			correct, alreadyAnswered, idx := r.answer(msg.Player, answer)
			if !correct {
				text := fmt.Sprintf(T("%q salah, sisa waktu %s"), answer, r.timeLeft())
				g.Out <- Message{GameID: g.ID, Kind: TextMessage, Text: text}
				continue
			}

			if alreadyAnswered {
				player := g.players[r.correct[idx]]
				text := fmt.Sprintf(T("%q telah di jawab oleh %s"), answer, player.Name)
				g.Out <- Message{GameID: g.ID, Kind: TextMessage, Text: text}
				continue
			}

			text := r.questionText(false)
			text += fmt.Sprintf("\n"+T("waktu tersisa %s lagi"), r.timeLeft())
			g.Out <- Message{GameID: g.ID, Kind: TextMessage, Text: text}

			if r.finised() {
				r.state = Finished
				timeLeftTick.Stop()
				g.Out <- Message{GameID: g.ID, Kind: StateMessage, Text: string(RoundFinished)}
				return nil
			}
		case <-timeLeftTick.C: // inform time left
			text := fmt.Sprintf(T("waktu tersisa %s lagi"), r.timeLeft())
			select {
			case g.Out <- Message{GameID: g.ID, Kind: TickMessage, Text: text}:
			default:
			}
		case <-timeout:
			g.State = Finished
			timeLeftTick.Stop()
			showUnAnswered := true
			text := fmt.Sprintf("%s\n\n%s", T("Waktu habis.."), r.questionText(showUnAnswered))
			g.Out <- Message{GameID: g.ID, Kind: StateMessage, Text: string(RoundFinished)}
			g.Out <- Message{GameID: g.ID, Kind: TextMessage, Text: text}
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

func newRound(seed int64, roundPlayed int, players map[PlayerID]Player) (*round, error) {
	q, err := NextQuestion(seed, roundPlayed)
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

func (r *round) questionText(showUnAnswered bool) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[id: %d] %s?\n\n", r.q.id, r.q.text)
	for i, a := range r.q.answers {
		if pID := r.correct[i]; pID != "" {
			fmt.Fprintf(w, "%d. %-30s [ %2d ] - %s\n", i+1, a.String(), a.score, r.players[pID].Name)
		} else {
			if showUnAnswered {
				fmt.Fprintf(w, "%d. %-30s [ %2d ]\n", i+1, a.String(), a.score)
			} else {
				fmt.Fprintf(w, "%d. _________________________\n", i+1)
			}
		}
	}
	w.Flush()

	return b.String()
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
	if r.state != Started {
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
