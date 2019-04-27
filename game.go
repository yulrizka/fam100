package fam100

import (
	"math/rand"
	"strconv"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/uber-go/zap"
	"github.com/yulrizka/fam100/model"
	"github.com/yulrizka/fam100/qna"
	"github.com/yulrizka/fam100/repo"
)

// Game configuration
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
	Player     model.Player
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
	Rank   model.Rank
	Final  bool
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
	id               int64
	ChanID           string
	State            State
	totalRoundPlayed int
	players          map[model.PlayerID]model.Player
	chanName         string
	seed             int64
	rank             model.Rank
	currentRound     *round
	questionDB       qna.Provider

	In  chan Message
	Out chan Message
}

// NewGame create a new round
func NewGame(chanID, chanName string, in, out chan Message, questionDB qna.Provider) (r *Game, err error) {

	seed, totalRoundPlayed, err := repo.DefaultDB.NextGame(chanID)
	if err != nil {
		return nil, err
	}

	return &Game{
		id:               int64(rand.Int31()),
		ChanID:           chanID,
		chanName:         chanName,
		State:            Created,
		players:          make(map[model.PlayerID]model.Player),
		seed:             seed,
		totalRoundPlayed: totalRoundPlayed,
		In:               in,
		Out:              out,
		questionDB:       questionDB,
	}, err
}

// Start the game
func (g *Game) Start() {
	g.State = Started
	log.Info("Game started",
		zap.String("chanID", g.ChanID),
		zap.Int64("gameID", g.id),
		zap.Int64("seed", g.seed),
		zap.Int("totalRoundPlayed", g.totalRoundPlayed))

	go func() {
		g.Out <- StateMessage{ChanID: g.ChanID, State: Started, GameID: g.id}
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
		g.Out <- StateMessage{ChanID: g.ChanID, State: Finished, GameID: g.id}
		log.Info("Game finished", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.id))
	}()
}

func (g *Game) startRound(currentRound int) error {
	g.totalRoundPlayed++
	if err := repo.DefaultDB.IncRoundPlayed(g.ChanID); err != nil {
		log.Error("failed to increase totalRoundPlayed", zap.Int("totalRoundPlayed", g.totalRoundPlayed), zap.Error(err))
	}

	questionLimit := DefaultQuestionLimit
	if limitConf, err := repo.DefaultDB.ChannelConfig(g.ChanID, "questionLimit", ""); err == nil && limitConf != "" {
		if limit, err := strconv.ParseInt(limitConf, 10, 64); err == nil {
			questionLimit = int(limit)
		}
	}

	question, err := g.questionDB.NextQuestion(g.seed, g.totalRoundPlayed, questionLimit)
	if err != nil {
		return errors.Wrap(err, "failed to get the next question")
	}

	r, err := newRound(question, g.players)
	if err != nil {
		return err
	}

	g.currentRound = r
	r.state = RoundStarted
	timeUp := time.After(RoundDuration)
	timeLeftTick := time.NewTicker(tickDuration)
	displayAnswerTick := time.NewTicker(tickDuration)

	// print question
	g.Out <- StateMessage{ChanID: g.ChanID, State: RoundStarted, Round: currentRound, RoundText: r.questionText(g.ChanID, false), GameID: g.id}
	log.Info("Round Started", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.id), zap.Int64("roundID", r.id), zap.Int("questionID", r.q.ID), zap.Int("questionLimit", questionLimit))

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

			if r.finished() {
				timeLeftTick.Stop()
				displayAnswerTick.Stop()
				g.showAnswer(r)
				r.state = RoundFinished
				err := g.updateRanking(r.ranking())
				if err != nil {
					log.Error("failed to update ranking", zap.Error(err))
				}

				g.Out <- StateMessage{ChanID: g.ChanID, State: RoundFinished, Round: currentRound, GameID: g.id}
				log.Info("Round finished", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.id), zap.Int64("roundID", r.id), zap.Bool("timeout", false))
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
			err := g.updateRanking(r.ranking())
			if err != nil {
				log.Error("failed to update ranking", zap.Error(err))
			}

			g.Out <- StateMessage{ChanID: g.ChanID, State: RoundTimeout, Round: currentRound, GameID: g.id}
			log.Info("Round finished", zap.String("chanID", g.ChanID), zap.Int64("gameID", g.id), zap.Int64("roundID", r.id), zap.Bool("timeout", true))
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
		zap.Int64("gameID", g.id),
		zap.Int64("roundID", r.id))

	return false
}

func (g *Game) updateRanking(r model.Rank) error {
	g.rank = g.rank.Add(r)
	err := repo.DefaultDB.SaveScore(g.ChanID, g.chanName, r)

	return errors.Wrap(err, "failed to save score")
}

func (g *Game) CurrentQuestion() qna.Question {
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
