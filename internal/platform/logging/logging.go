package logging

import (
	"log"
	"os"
)

type Logger struct {
	*log.Logger
}

func New(service string) *Logger {
	prefix := service + " "
	return &Logger{Logger: log.New(os.Stdout, prefix, log.LstdFlags|log.Lmicroseconds|log.LUTC)}
}

func (l *Logger) Info(msg string, kv ...any) {
	l.Printf("INFO  %s %v", msg, kv)
}

func (l *Logger) Error(msg string, kv ...any) {
	l.Printf("ERROR  %s %v", msg, kv)
}
