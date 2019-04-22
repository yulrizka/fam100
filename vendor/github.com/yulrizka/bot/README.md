# bot

Is a mini framework that alow you to create a plugin that works with multiple platform

# Feature
* Work with **Slack** and **Telegram**
* Plugin as middleware. Multiple plugin can be combined together
* Upload file (☑︎Slack, 🗹Telegram)

## Example

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/yulrizka/bot"
)

// marcoPolo is an example plugin that will reply text marco with polo
type marcoPolo struct {
	cl  bot.Client
	out chan bot.Message
}

func (*marcoPolo) Name() string {
	return "MarcoPolo"
}

// Init should store the out channel to send message and do initialization
func (m *marcoPolo) Init(out chan bot.Message, cl bot.Client) error {
	m.out = out
	m.cl = cl
	return nil
}

// Handle incoming message that could be in any type (*bot.Message, *bot.JoinMessage, etc).
// return handled false will pass modifiedMsg to other plugins down the chain
func (m *marcoPolo) Handle(rawMsg interface{}) (handled bool, modifiedMsg interface{}) {
	if inMessage, ok := rawMsg.(*bot.Message); ok {
		if strings.TrimSpace(strings.ToLower(inMessage.Text)) == "marco" {
			text := fmt.Sprintf("POLO! -> %s (<@%s>)\n", inMessage.From.FullName(), inMessage.From.Username)
			// send message
			msg := bot.Message{
				Chat: inMessage.Chat,
				Text: text,
			}
			m.out <- msg
		}
	}

	// handled true will stop exit the middleware chain. Handle method of the next plugin will not be called
	// modifiedMsg give plugin a chance to modify the message for the next plugin
	handled, modifiedMsg = false, rawMsg
	return
}

func init() {
	// Log message is callback method that gives you chance to handle the log using your preferred library
	bot.Log = func(record bot.LogRecord) {
		log.Print(record)
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	key := os.Getenv("SLACK_KEY")
	if key == "" {
		panic("SLACK_KEY can not be empty")
	}
	var client bot.Client
	var err error

	client, err = bot.NewSlack(context.Background(), key)
	if err != nil {
		log.Fatal(err)
	}
	plugin := new(marcoPolo)
	if err := client.AddPlugins(plugin); err != nil {
		panic(err)
	}

	client.Start()
}
```

## Project using this library

* [fam100](https://github.com/yulrizka/fam100) a game of family feud

