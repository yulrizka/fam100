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

// Message to communitace with round
type Message struct {
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

// RoundState represents state of the round
type RoundState string

// RoundState Kind
const (
	Created  RoundState = "created"
	Started  RoundState = "started"
	Finished RoundState = "finished"
)

// Round represent one question round
type Round struct {
	q       Question
	correct []PlayerID // correct answer answered by a player, "" means not answered
	state   RoundState
	players map[PlayerID]Player
	inbox   <-chan Message
	outbox  chan Message
}

// NewRound create a new round
func NewRound(q Question, inbox <-chan Message) (r *Round, outbox <-chan Message) {
	r = &Round{
		q:       q,
		correct: make([]PlayerID, len(q.answers)),
		state:   Created,
		players: make(map[PlayerID]Player),
		inbox:   inbox,
		outbox:  make(chan Message),
	}
	return r, r.outbox
}

// Start the round
func (g *Round) Start() {
	g.state = Started
	go func() {
		timeout := time.After(roundDuration)
		timeoutAt := time.Now().Add(roundDuration).Round(time.Second)
		tick := time.NewTicker(tickDuration)

		// print question
		g.outbox <- Message{Kind: StateMessage, Text: string(Started)}

		qText := g.questionText(false)
		qText += "\n"
		qText += fmt.Sprintf(i("Anda memiliki waktu %s"), roundDuration)
		g.outbox <- Message{Kind: TextMessage, Text: qText}

		for {
			select {
			case msg := <-g.inbox:
				answer := msg.Text
				correct, answered, _, idx := g.answer(msg.Player, answer)
				if !correct {
					text := fmt.Sprintf(i("%q salah, sisa waktu %s"), answer, timeLeft(timeoutAt))
					g.outbox <- Message{Kind: TextMessage, Text: text}
					continue
				}

				if answered {
					player := g.players[g.correct[idx]]
					text := fmt.Sprintf(i("%q telah di jawab oleh %s"), answer, player.Name)
					g.outbox <- Message{Kind: TextMessage, Text: text}
					continue
				} else {
					text := g.questionText(false)
					text += fmt.Sprintf("\n"+i("waktu tersisa %s lagi"), timeLeft(timeoutAt))
					g.outbox <- Message{Kind: TextMessage, Text: text}
				}

				if g.finised() {
					g.state = Finished
					tick.Stop()
					g.outbox <- Message{Kind: StateMessage, Text: string(Finished)}
				}
			case <-tick.C:
				text := fmt.Sprintf(i("waktu tersisa %s lagi"), timeLeft(timeoutAt))
				select {
				case g.outbox <- Message{Kind: TickMessage, Text: text}:
				default:
				}
			case <-timeout:
				// waktu habis
				g.state = Finished
				tick.Stop()
				showUnAnswered := true
				text := fmt.Sprintf("%s\n\n%s", i("Waktu habis.."), g.questionText(showUnAnswered))
				g.outbox <- Message{Kind: TextMessage, Text: text}
				g.outbox <- Message{Kind: StateMessage, Text: string(Finished)}
			}
		}
	}()
}

// Stop the round
func (g *Round) Stop() {
	g.state = Finished
}

func (g *Round) player(id PlayerID) (Player, bool) {
	p, ok := g.players[id]
	return p, ok
}

func (g *Round) questionText(showUnAnswered bool) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[%d] %s?\n\n", g.q.id, g.q.text)
	for i, a := range g.q.answers {
		if pID := g.correct[i]; pID != "" {
			fmt.Fprintf(w, "%d. %-30s [ %2d ] - %s\n", i+1, a.String(), a.score, g.players[pID].Name)
		} else {
			if showUnAnswered {
				fmt.Fprintf(w, "%d. %-30s [ %2d ]\n", i+1, a.String(), a.score)
			} else {
				fmt.Fprintf(w, "%d. ______________________________\n", i+1)
			}
		}
	}
	w.Flush()

	return b.String()
}

func (g *Round) finised() bool {
	answered := 0
	for _, pID := range g.correct {
		if pID != "" {
			answered++
		}
	}

	return answered == len(g.q.answers)
}

func (g *Round) scores() map[PlayerID]int {
	scores := make(map[PlayerID]int)
	for i, pID := range g.correct {
		if pID != "" {
			scores[pID] = g.q.answers[i].score
		}
	}
	return scores
}

func (g *Round) answer(p Player, text string) (correct, answered bool, score, index int) {
	if g.state != Started {
		return false, false, 0, -1
	}

	if correct, score, i := g.q.checkAnswer(text); correct {
		if g.correct[i] != "" {
			// already answered
			return correct, true, score, i
		}
		g.correct[i] = p.ID
		g.players[p.ID] = p

		return correct, false, score, i
	}
	return false, false, 0, -1
}

func timeLeft(endAt time.Time) time.Duration {
	return endAt.Sub(time.Now().Round(time.Second))
}
