package alert

import (
	"time"
)

// Alert Interface for sending alerts
type Alert interface {
	Send(data Data) error
	Validate() error
}

type AlertLevelType string

const (
	AlertError AlertLevelType = "Error"
	AlertInfo  AlertLevelType = "Info"
)

// Data structure available for the alerter
type Data struct {
	Name      string
	Message   string
	Status    string
	StartTime time.Time
	EndTime   time.Time
	ExitCode  int
	Namespace string
	Config    map[string]string
	Level     AlertLevelType
}
