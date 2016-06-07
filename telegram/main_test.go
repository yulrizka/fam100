package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yulrizka/bot"
	"github.com/yulrizka/fam100"
)

func TestMain(m *testing.M) {
	if err := fam100.LoadQuestion("fam100.db"); err != nil {
		panic(err)
	}
	if err := initRedis(); err != nil {
		panic(err)
	}
	retCode := m.Run()
	fam100.DB.Close()
	os.Exit(retCode)
}

func TestQuorumShouldStartGame(t *testing.T) {
	// create a new game
	out := make(chan bot.Message)
	b := fam100Bot{}
	in, err := b.Init(out)
	if err != nil {
		t.Error(err)
	}

	chID := "1"
	b.start()
	// send join message 3 time from the same person
	msg := bot.Message{
		From: bot.User{ID: "1", FirstName: "Foo"},
		Chat: bot.Chat{ID: chID, Type: bot.Group},
		Text: "/join@" + botName,
	}
	for i := 0; i < 3; i++ {
		in <- &msg
	}
	readOutMessage(&b)
	g, ok := b.channels[chID]
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
	in <- &bot.Message{
		From: bot.User{ID: "2", FirstName: "Foo"},
		Chat: bot.Chat{ID: "2", Type: bot.Group},
		Text: "/join@" + botName,
	}
	in <- &bot.Message{
		From: bot.User{ID: "3", FirstName: "Foo"},
		Chat: bot.Chat{ID: "2", Type: bot.Group},
		Text: "/join@" + botName,
	}
	readOutMessage(&b)
	if want, got := fam100.Created, g.game.State; want != got {
		t.Fatalf("state want %s, got %s", want, got)
	}
	if want, got := 1, len(g.quorumPlayer); want != got {
		t.Fatalf("quorum want %d, got %d", want, got)
	}

	// message with quorum should start the game
	in <- &bot.Message{
		From: bot.User{ID: "4", FirstName: "Foo"},
		Chat: bot.Chat{ID: chID, Type: bot.Group},
		Text: "/join@" + botName,
	}
	readOutMessage(&b)
	in <- &bot.Message{
		From: bot.User{ID: "5", FirstName: "Foo"},
		Chat: bot.Chat{ID: chID, Type: bot.Group},
		Text: "/join@" + botName,
	}
	readOutMessage(&b)
	if want, got := fam100.Started, g.game.State; want != got {
		t.Fatalf("state want %s, got %s", want, got)
	}
	if want, got := 3, len(g.quorumPlayer); want != got {
		t.Fatalf("quorum want %d, got %d", want, got)
	}
}

func readOutMessage(b *fam100Bot) (fam100.Message, error) {
	for {
		select {
		case m := <-b.out:
			return m, nil
		case <-time.After(1 * time.Second):
			return nil, fmt.Errorf("timeout waiting to message")
		}
	}
}
