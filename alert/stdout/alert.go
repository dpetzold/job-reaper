package stdout

import (
	"errors"
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/sstarcher/job-reaper/alert"
)

// Service structure for stdout
type Service struct {
	Level string
}

// Validate sensu configuration
func (s Service) Validate() error {
	if s.Level == "info" || s.Level == "error" {
		return nil
	}
	return errors.New("level must be info or error")
}

// Send alert to stdout
func (s Service) Send(data alert.Data) error {

	switch data.Level {
	case alert.AlertInfo:
		value := fmt.Sprintf("%s with exit code [%d] for %s", data.Status, data.ExitCode, data.Message)
		log.Infof("[%s in %s] Reaping @ [%s] @ %s", data.Name, data.Namespace, value, data.EndTime.String())
	case alert.AlertError:
		if s.Level == "error" {
			log.Errorf("%s %s %s %s", data.Name, data.Namespace, data.Message, data.EndTime.String())
		}
	}

	return nil
}
