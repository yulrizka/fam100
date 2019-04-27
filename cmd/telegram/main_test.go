package main

import (
	"context"
	"fmt"
	l "log"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/uber-go/zap"
	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
	"github.com/yulrizka/fam100/qna"
	"github.com/yulrizka/fam100/repo"
)

func Test_Main(t *testing.T) {
	repo.RedisPrefix = "test_fam100"
	if err := repo.DefaultDB.Init(); err != nil {
		t.Fatalf("failed to load database: %v", err)
	}

	if err := repo.DefaultDB.Reset(); err != nil {
		t.Fatalf("failed to reset database: %v", err)
	}

	t.Run("quorum should start the game", testQuorumShouldStartGame)
	t.Run("test game", testGame)
}

func testQuorumShouldStartGame(t *testing.T) {
	questionDB, err := qna.NewBolt("../qna/test.db")
	if err != nil {
		t.Fatalf("failed to load questions: %v", err)
	}
	defer questionDB.Close()

	oMinQuorum := minQuorum
	defer func() {
		minQuorum = oMinQuorum
	}()
	minQuorum = 2
	setupLogger(zap.InfoLevel)
	fam100.SetLogger(log)

	ctx := context.Background()

	// create a new game
	out := make(chan bot.Message)
	b := fam100Bot{
		name:  "fam100bot",
		qnaDB: questionDB,
	}

	// for testing we set channel buffer to only one message
	// so if we send 2 message (command then noop), once the previous message unblock,
	// we know that 1st message was processed
	telegramInBufferSize = 1
	noop := bot.Message{}

	err = b.Init(ctx, out, nil)
	if err != nil {
		t.Fatalf("failed to initialize bot: %v", err)
	}

	chanID := "1"
	player1 := bot.User{ID: "ID1", FirstName: "Player 1"}
	player2 := bot.User{ID: "ID2", FirstName: "Player 2"}
	players := []bot.User{player1, player2}

	// send join message 3 time from the same person
	msg := bot.Message{
		From: player1,
		Chat: bot.Chat{ID: chanID, Type: bot.Group},
		Text: "/join@" + b.name,
	}
	for i := 0; i < 3; i++ {
		b.in <- &msg
	}
	b.in <- noop

	// game should not started
	g, ok := b.channels[chanID]
	if !ok {
		t.Fatalf("failed to get channel")
	}
	if want, got := 1, len(g.quorumPlayer); want != got {
		t.Fatalf("quorum want %d, got %d", want, got)
	}
	if want, got := fam100.Created, g.game.State; want != got {
		t.Fatalf("state want %s, got %s", want, got)
	}

	// message to another channel, should not affect the state
	b.in <- &bot.Message{
		From: player2,
		Chat: bot.Chat{ID: "2", Type: bot.Group},
		Text: "/join@" + b.name,
	}
	b.in <- noop
	if want, got := fam100.Created, g.game.State; want != got {
		t.Fatalf("state want %s, got %s", want, got)
	}
	if want, got := 1, len(g.quorumPlayer); want != got {
		t.Fatalf("quorum want %d, got %d", want, got)
	}

	// message with quorum should start the game
	b.in <- &bot.Message{
		From: bot.User{ID: "4", FirstName: "Foo"},
		Chat: bot.Chat{ID: chanID, Type: bot.Group},
		Text: "/join@" + b.name,
	}
	b.in <- noop

	// game started with the first question
	reply := readOutMessage(t, &b)
	t.Logf("question: %v", reply)
	if _, ok := reply.(bot.Message); !ok {
		t.Fatalf("expecting message got %v", reply)
	}
	if want, got := fam100.Started, g.game.State; want != got {
		t.Fatalf("state want %s, got %s", want, got)
	}
	if want, got := minQuorum, len(g.quorumPlayer); want != got {
		t.Fatalf("quorum want %d, got %d", want, got)
	}

	fam100.DelayBetweenRound = 0

	for i := 1; i <= fam100.RoundPerGame; i++ {
		if i > 1 {
			// next round question
			reply := readOutMessage(t, &b)
			t.Logf("next question: %v", reply)
			if _, ok := reply.(bot.Message); !ok {
				t.Fatalf("expecting message got %v", reply)
			}
		}
		question := g.game.CurrentQuestion()
		for _, ans := range question.Answers {
			b.in <- &bot.Message{
				From: players[rand.Intn(len(players))],
				Chat: bot.Chat{ID: chanID, Type: bot.Group},
				Text: ans.Text[0],
			}
			b.in <- noop
		}

		// all question was answered

		// all answers
		reply = readOutMessage(t, &b)
		t.Logf("answer results = %+v\n", reply) // for debugging
		if _, ok := reply.(bot.Message); !ok {
			t.Fatalf("expecting message got %v", reply)
		}

		// ranking
		reply = readOutMessage(t, &b)
		t.Logf("rank = %+v\n", reply) // for debugging
		if _, ok := reply.(bot.Message); !ok {
			t.Fatalf("expecting message got %v", reply)
		}

	}

	// Game selesai
	if want, got := fam100.Finished, g.game.State; want != got {
		t.Fatalf("state want %s, got %s", want, got)
	}
}

func readOutMessage(t *testing.T, b *fam100Bot) fam100.Message {
	t.Helper()
	for {
		select {
		case m := <-b.out:
			return m
		case <-time.After(1 * time.Second):
			t.Fatal(fmt.Errorf("timeout waiting to message"))
			return nil
		}
	}
}

func testGame(t *testing.T) {
	t.Skip()

	out := make(chan bot.Message)
	b := fam100Bot{}
	botName := "fam100bot"

	err := b.Init(context.Background(), out, nil)
	if err != nil {
		t.Error(err)
	}

	// read output
	go func() {
		for {
			<-b.out
		}
		//time.Sleep(70 * time.Millisecond)
	}()

	players := make([]bot.User, 5)
	for i := 0; i < len(players); i++ {
		players[i] = bot.User{ID: fmt.Sprintf("%d", i), FirstName: fmt.Sprintf("Player %d", i)}
	}

	play := func(chatID string) {
		// send join message from 3 different user
		for i := 0; i < 3; i++ {
			b.in <- &bot.Message{
				From:       players[i],
				Chat:       bot.Chat{ID: chatID, Type: bot.Group},
				Text:       "/join@" + botName,
				ReceivedAt: time.Now(),
			}
		}

		// randomly answer message at a rate
		for {
			select {
			default:
				b.in <- &bot.Message{
					From:       players[rand.Intn(len(players))],
					Chat:       bot.Chat{ID: chatID, Type: bot.Group},
					Text:       "some answer",
					ReceivedAt: time.Now(),
				}
				b.in <- &bot.Message{
					From: players[rand.Intn(len(players))],
					Chat: bot.Chat{ID: chatID, Type: bot.Group},
					//Text:       "foo",
					Text:       "/join@" + botName,
					ReceivedAt: time.Now(),
				}
			}
		}
	}

	setupLogger(zap.WarnLevel)
	fam100.SetLogger(log)
	for i := 0; i < 500; i++ {
		go play(fmt.Sprintf("%d", i))
	}

	time.Sleep(5 * time.Second)

	go metrics.LogScaled(metrics.DefaultRegistry, 1*time.Second, time.Millisecond, l.New(os.Stderr, "", 0))
	time.Sleep(1*time.Second + 100*time.Millisecond)
}
