// Package events carries the progress of long operations (deploy, update,
// delete…) to whoever started them: the CLI renders events as text, the
// API streams them to the browser.
package events

import (
	"fmt"
	"io"
	"time"
)

// Level classifies an event.
type Level string

const (
	LevelStep   Level = "step"   // a phase starts: Stage + Message
	LevelInfo   Level = "info"   // neutral information
	LevelWarn   Level = "warn"   // non-fatal problem
	LevelDetail Level = "detail" // key/value context: Key + Message
	LevelOk     Level = "ok"     // success
)

// Event is one progress notification.
type Event struct {
	Time    time.Time `json:"time"`
	Level   Level     `json:"level"`
	Stage   string    `json:"stage,omitempty"`
	Key     string    `json:"key,omitempty"`
	Message string    `json:"message"`
}

// Progress receives events and the raw output of subprocesses (docker,
// git). The zero value discards everything.
type Progress struct {
	Emit func(Event)
	Out  io.Writer
}

// Writer returns where subprocess output goes.
func (p Progress) Writer() io.Writer {
	if p.Out == nil {
		return io.Discard
	}
	return p.Out
}

func (p Progress) emit(e Event) {
	if p.Emit == nil {
		return
	}
	e.Time = time.Now()
	p.Emit(e)
}

func (p Progress) Step(stage, format string, args ...any) {
	p.emit(Event{Level: LevelStep, Stage: stage, Message: fmt.Sprintf(format, args...)})
}

func (p Progress) Info(format string, args ...any) {
	p.emit(Event{Level: LevelInfo, Message: fmt.Sprintf(format, args...)})
}

func (p Progress) Warn(format string, args ...any) {
	p.emit(Event{Level: LevelWarn, Message: fmt.Sprintf(format, args...)})
}

func (p Progress) Detail(key, value string) {
	p.emit(Event{Level: LevelDetail, Key: key, Message: value})
}

func (p Progress) Ok(format string, args ...any) {
	p.emit(Event{Level: LevelOk, Message: fmt.Sprintf(format, args...)})
}
