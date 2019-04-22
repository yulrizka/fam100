package bot

import "fmt"

type ErrorLevel int

// Error level in order of value Error < Info < Warn < Debug
const (
	Error ErrorLevel = iota
	Info
	Warn
	Debug
)

type LogRecord struct {
	Message string
	Level   ErrorLevel
}

func (l LogRecord) String() string {
	var levelStr string
	switch l.Level {
	case Error:
		levelStr = "ERROR"
	case Info:
		levelStr = "INFO"
	case Warn:
		levelStr = "WARN"
	case Debug:
		levelStr = "DEBUG"
	}

	return fmt.Sprintf("[%s][bot] %s", levelStr, l.Message)
}

// Log function that will be called for logging. By default it's null logger, client can override this
// to implement their logging of their choice
var Log = func(record LogRecord) {}

func log(level ErrorLevel, msg string) {
	Log(LogRecord{Level: level, Message: msg})
}
