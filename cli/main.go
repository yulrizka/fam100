package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/uber-go/zap"
	"github.com/yulrizka/fam100"
)

var (
	input       = make(chan string)
	name        string
	roundPlayed int
	seed        int64

	log    = zap.NewJSON()
	dbPath = "fam100.db"
)

func main() {
	// setup logging
	flag.StringVar(&dbPath, "db", "fam100.db", "question database")
	logLevel := zap.LevelFlag("v", zap.ErrorLevel, "log level: all, debug, info, warn, error, panic, fatal, none")
	flag.Parse()
	log.SetLevel(*logLevel)
	zap.AddCaller()

	fam100.SetLogger(log)

	fam100.DefaultDB = &fam100.MemoryDB{Seed: 0}
	fam100.TickAfterWrongAnswer = true

	// setup question DB
	n, err := fam100.InitQuestion(dbPath)
	if err != nil {
		log.Fatal("Failed loading question DB", zap.Error(err))
	} else {
		log.Info("Question loaded", zap.Int("nQuestion", n))
	}
	defer func() {
		if r := recover(); r != nil {
			fam100.DefaultQuestionDB.Close()
			panic(r)
		}
		fam100.DefaultQuestionDB.Close()
	}()
	fam100.RoundPerGame = n

	printHeader()
	seed = time.Now().UnixNano()

	scanner := bufio.NewScanner(os.Stdin)
	for name == "" {
		fmt.Print("Siapa nama Anda? ")
		scanner.Scan()
		name = strings.TrimSpace(scanner.Text())
	}

	fmt.Printf("\nHalo %s ketik '/keluar' kapanpun jika ingin berhenti!\n\n", name)
	go startGame()
	time.Sleep(300 * time.Millisecond)

	for scanner.Scan() {
		fmt.Print("> ")
		text := scanner.Text()
		if text == "/keluar" {
			os.Exit(0)
		}

		select {
		case input <- text:
		default:
		}
	}
}

func printHeader() {
	fmt.Println()
	fmt.Println("===============")
	fmt.Println("Famili 100 v0.1")
	fmt.Println("===============")
	fmt.Println()
}

func startGame() {
	for {
		fmt.Printf("Siap ? (y/n) ")
		ya := []string{"ya", "y", "yes"}
	READY:
		for i := range input {
			i = strings.TrimSpace(strings.ToLower(i))
			for _, v := range ya {
				if i == v {
					break READY
				}
			}
			fmt.Print("Siap ? (y/n) ")
		}
		fmt.Println()

		in := make(chan fam100.Message)
		out := make(chan fam100.Message)
		game, _ := fam100.NewGame("cli", "cli", in, out)
		game.Start()

		for {
			select {
			case m := <-out:
				//fmt.Printf("m = %+v\n", m)
				switch msg := m.(type) {
				case fam100.StateMessage:
					if msg.State == fam100.RoundStarted {
						fmt.Println(formatQNA(msg.RoundText))
						fmt.Println()
					}
				case fam100.QNAMessage:
					fmt.Println(formatQNA(msg))
					fmt.Println()
				case fam100.WrongAnswerMessage:
					fmt.Printf("salah, sisa waktu %s\n", msg.TimeLeft)
				case fam100.RankMessage:
					var score = 0
					if len(msg.Rank) > 0 {
						score = msg.Rank[0].Score
					}
					fmt.Printf("Total Score > %d\n\n", score)
				default:
					//fmt.Printf("msg = %+v\n", msg)
				}
			case i := <-input:
				msg := fam100.TextMessage{
					Player: fam100.Player{ID: fam100.PlayerID(name), Name: name},
					Text:   i,
				}
				in <- msg
			}
		}
	}
}

func formatQNA(msg fam100.QNAMessage) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	fmt.Fprintf(w, "[%d] %s?\n\n", msg.QuestionID, msg.QuestionText)
	for i, a := range msg.Answers {
		if a.Answered {
			fmt.Fprintf(w, "%d. %-30s [ %2d ] - %s\n", i+1, a.Text, a.Score, a.PlayerName)
		} else {
			if msg.ShowUnanswered {
				fmt.Fprintf(w, "%d. %-30s [ %2d ]\n", i+1, a.Text, a.Score)
			} else {
				fmt.Fprintf(w, "%d. ______________________________\n", i+1)
			}
		}
	}
	w.Flush()

	return b.String()
}
