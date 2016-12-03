package fam100

import (
	"math/rand"
	"sort"
	"strconv"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/uber-go/zap"
)

// Bot configuration
var (
	RoundDuration        = 90 * time.Second
	tickDuration         = 10 * time.Second
	DelayBetweenRound    = 5 * time.Second
	TickAfterWrongAnswer = false
	RoundPerGame         = 3
	DefaultQuestionLimit = 600
	log                  zap.Logger

	playerActiveMap = cache.New(5*time.Minute, 30*time.Second)
)

func init() {
	log = zap.New(zap.NewJSONEncoder())
	go func() {
		for range time.Tick(30 * time.Second) {
			playerActive.Update(int64(playerActiveMap.ItemCount()))
		}
	}()
}

func SetLogger(l zap.Logger) {
	log = l.With(zap.String("module", "fam100"))
}

// Message to communicate between player and the game
type Message interface{}

// TextMessage represents a chat message
type TextMessage struct {
	ChanID     string
	Player     Player
	Text       string
	ReceivedAt time.Time
}

// StateMessage represents state change in the game
type StateMessage struct {
	GameID    int64
	ChanID    string
	Round     int
	State     State
	RoundText QNAMessage //question and answer
}

// TickMessage represents time left notification
type TickMessage struct {
	ChanID   string
	TimeLeft time.Duration
}

type WrongAnswerMessage TickMessage

// QNAMessage represents question and answer for a round
type QNAMessage struct {
	ChanID         string
	Round          int
	QuestionText   string
	QuestionID     int
	Answers        []roundAnswers
	ShowUnanswered bool // reveal un-answered question (end of round)
	TimeLeft       time.Duration
}

type roundAnswers struct {
	Text       string
	Score      int
	Answered   bool
	PlayerName string
	Highlight  bool
}
type RankMessage struct {
	ChanID string
	Round  int
	Rank   Rank
	Final  bool
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

// Available state
const (
	Created       State = "created"
	Started       State = "started"
	Finished      State = "finished"
	RoundStarted  State = "roundStarted"
	RoundTimeout  State = "RoundTimeout"
	RoundFinished State = "roundFinished"
)

// Game can consists of multiple round
// each round user will be asked question and gain points
type Game struct {
	ID               int64
	ChanID           string
	ChanName         string
	State            State
	TotalRoundPlayed int
	players          map[PlayerID]Player
	seed             int64
	rank             Rank
	currentRound     *round

	In  chan Message
	Out chan Message
}

// NewGame create a new round
func NewGame(chanID, chanName string, in, out chan Message) (r *Game, err error) {
	seed, totalRoundPlayed, err := DefaultDB.nextGame(chanID)
	if err != nil {
		return nil, err
	}

	return &Game{
		ID:               int64(rand.Int31()),
		ChanID:           chanID,
		ChanName:         chanName,
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
	log.Info("Game started",
		zap.String("chanID", g.ChanID),
		zap.Int64("gameID", g.ID),
		zap.Int64("seed", g.seed),
		zap.Int("totalRoundPlayed", g.TotalRoundPlayed))

	go func() {
		g.Out <- StateMessage{ChanID: g.ChanID, State: Started, GameID: g.ID}
		for i := 1; i <= RoundPerGame; i++ {
			err := g.startRound(i)
			if err != nil {
				log.Error("starting round failed", zap.String("chanID", g.ChanID), zap.Error(err))
			}
			final := i == RoundPerGame
			g.Out <- RankMessage{ChanID: g.ChanID, Round: i, Rank: g.rank, Final: final}
			if !final {
				time.Sleep(DelayBetweenRound)
			}
		}
		g.State = Finished
		g.Out <- StateMessage{ChanID: g.ChanID, State: Finished, GameID: g.ID}
		log.Info("Game finished", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.ID))
	}()
}

func (g *Game) startRound(currentRound int) error {
	g.TotalRoundPlayed++
	if err := DefaultDB.incRoundPlayed(g.ChanID); err != nil {
		log.Error("failed to increase totalRoundPlayed", zap.Int("totalRoundPlayed", g.TotalRoundPlayed), zap.Error(err))
	}

	questionLimit := DefaultQuestionLimit
	if limitConf, err := DefaultDB.ChannelConfig(g.ChanID, "questionLimit", ""); err == nil && limitConf != "" {
		if limit, err := strconv.ParseInt(limitConf, 10, 64); err == nil {
			questionLimit = int(limit)
		}
	}

	r, err := newRound(g.seed, g.TotalRoundPlayed, g.players, questionLimit)
	if err != nil {
		return err
	}

	g.currentRound = r
	r.state = RoundStarted
	timeUp := time.After(RoundDuration)
	timeLeftTick := time.NewTicker(tickDuration)
	displayAnswerTick := time.NewTicker(tickDuration)

	// print question
	g.Out <- StateMessage{ChanID: g.ChanID, State: RoundStarted, Round: currentRound, RoundText: r.questionText(g.ChanID, false), GameID: g.ID}
	log.Info("Round Started", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.ID), zap.Int64("roundID", r.id), zap.Int("questionID", r.q.ID), zap.Int("questionLimit", questionLimit))

	for {
		select {
		case rawMsg := <-g.In: // new answer coming from player
			started := time.Now()
			msg, ok := rawMsg.(TextMessage)
			if !ok {
				log.Error("Unexpected message type input from client")
				continue
			}
			gameLatencyTimer.UpdateSince(msg.ReceivedAt)

			handled := g.handleMessage(msg, r)
			if handled {
				gameMsgProcessTimer.UpdateSince(started)
				gameServiceTimer.UpdateSince(msg.ReceivedAt)
				continue
			}

			if r.finised() {
				timeLeftTick.Stop()
				displayAnswerTick.Stop()
				g.showAnswer(r)
				r.state = RoundFinished
				g.updateRanking(r.ranking())
				g.Out <- StateMessage{ChanID: g.ChanID, State: RoundFinished, Round: currentRound, GameID: g.ID}
				log.Info("Round finished", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.ID), zap.Int64("roundID", r.id), zap.Bool("timeout", false))
				gameFinishedTimer.UpdateSince(started)

				return nil
			}
			gameMsgProcessTimer.UpdateSince(started)
			gameServiceTimer.UpdateSince(msg.ReceivedAt)

		case <-timeLeftTick.C: // inform time left
			select {
			case g.Out <- TickMessage{ChanID: g.ChanID, TimeLeft: r.timeLeft()}:
			default:
			}

		case <-displayAnswerTick.C: // show correct answer (at most once every 10s)
			g.showAnswer(r)

		case <-timeUp: // time is up
			timeLeftTick.Stop()
			displayAnswerTick.Stop()
			g.State = RoundFinished
			g.updateRanking(r.ranking())
			g.Out <- StateMessage{ChanID: g.ChanID, State: RoundTimeout, Round: currentRound, GameID: g.ID}
			log.Info("Round finished", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.ID), zap.Int64("roundID", r.id), zap.Bool("timeout", true))
			showUnAnswered := true
			g.Out <- r.questionText(g.ChanID, showUnAnswered)

			return nil
		}
	}
}

func (g *Game) handleMessage(msg TextMessage, r *round) (handled bool) {
	playerActiveMap.Set(string(msg.Player.ID), struct{}{}, cache.DefaultExpiration)
	log.Debug("startRound got message", zap.String("chanID", g.ChanID), zap.Object("msg", msg))
	answer := msg.Text
	correct, alreadyAnswered, idx := r.answer(msg.Player, answer)
	if !correct {
		if TickAfterWrongAnswer {
			g.Out <- WrongAnswerMessage{ChanID: g.ChanID, TimeLeft: r.timeLeft()}
		}
		return true
	}
	if alreadyAnswered {
		log.Debug("already answered", zap.String("chanID", g.ChanID), zap.String("by", string(r.correct[idx])))
		return true
	}

	log.Info("answer correct",
		zap.String("playerID", string(msg.Player.ID)),
		zap.String("playerName", msg.Player.Name),
		zap.String("answer", answer),
		zap.Int("questionID", r.q.ID),
		zap.String("chanID", g.ChanID),
		zap.Int64("gameID", g.ID),
		zap.Int64("roundID", r.id))

	return false
}

func (g *Game) updateRanking(r Rank) {
	g.rank = g.rank.Add(r)
	DefaultDB.saveScore(g.ChanID, g.ChanName, r)
}

func (g *Game) CurrentQuestion() Question {
	return g.currentRound.q
}

func (g *Game) showAnswer(r *round) {
	var show bool
	// if there is no highlighted answer don't display
	for _, v := range r.highlight {
		if v {
			show = true
			break
		}
	}
	if !show {
		return
	}

	qnaText := r.questionText(g.ChanID, false)
	select {
	case g.Out <- qnaText:
	default:
	}

	for i := range r.highlight {
		r.highlight[i] = false
	}
}

// round represents with one question
type round struct {
	id        int64
	q         Question
	state     State
	correct   []PlayerID // correct answer answered by a player, "" means not answered
	players   map[PlayerID]Player
	highlight map[int]bool

	endAt time.Time
}

func newRound(seed int64, totalRoundPlayed int, players map[PlayerID]Player, questionLimit int) (*round, error) {
	q, err := NextQuestion(seed, totalRoundPlayed, questionLimit)
	if err != nil {
		return nil, err
	}

	return &round{
		id:        int64(rand.Int31()),
		q:         q,
		correct:   make([]PlayerID, len(q.Answers)),
		state:     Created,
		players:   players,
		highlight: make(map[int]bool),
		endAt:     time.Now().Add(RoundDuration).Round(time.Second),
	}, nil
}

func (r *round) timeLeft() time.Duration {
	return r.endAt.Sub(time.Now().Round(time.Second))
}

// questionText construct QNAMessage which contains questions, answers and score
func (r *round) questionText(gameID string, showUnAnswered bool) QNAMessage {
	ras := make([]roundAnswers, len(r.q.Answers))

	for i, ans := range r.q.Answers {
		ra := roundAnswers{
			Text:  ans.String(),
			Score: ans.Score,
		}
		if pID := r.correct[i]; pID != "" {
			ra.Answered = true
			ra.PlayerName = r.players[pID].Name
		}
		if r.highlight[i] {
			ra.Highlight = true
		}
		ras[i] = ra
	}

	msg := QNAMessage{
		ChanID:         gameID,
		QuestionText:   r.q.Text,
		QuestionID:     r.q.ID,
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

	return answered == len(r.q.Answers)
}

// ranking generates a rank for current round which contains player, answers and score
func (r *round) ranking() Rank {
	var roundScores Rank
	lookup := make(map[PlayerID]PlayerScore)
	for i, pID := range r.correct {
		if pID != "" {
			score := r.q.Answers[i].Score
			if ps, ok := lookup[pID]; !ok {
				lookup[pID] = PlayerScore{
					PlayerID: pID,
					Name:     r.players[pID].Name,
					Score:    score,
				}
			} else {
				ps = lookup[pID]
				ps.Score += score
				lookup[pID] = ps
			}
		}
	}

	for _, ps := range lookup {
		roundScores = append(roundScores, ps)
	}
	sort.Sort(roundScores)
	for i := range roundScores {
		roundScores[i].Position = i + 1
	}

	return roundScores
}

func (r *round) answer(p Player, text string) (correct, answered bool, index int) {
	if r.state != RoundStarted {
		return false, false, -1
	}

	if _, ok := r.players[p.ID]; !ok {
		r.players[p.ID] = p
	}
	if correct, _, i := r.q.checkAnswer(text); correct {
		if r.correct[i] != "" {
			// already answered
			return correct, true, i
		}
		r.correct[i] = p.ID
		r.highlight[i] = true

		return correct, false, i
	}
	return false, false, -1
}
