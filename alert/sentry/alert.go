package sentry

import (
	"errors"
	"fmt"

	log "github.com/Sirupsen/logrus"
	raven "github.com/getsentry/raven-go"
	"github.com/sstarcher/job-reaper/alert"
)

// Service structure for sentry
type Service struct {
	Dsn string
}

// Validate sentry configuration
func (s Service) Validate() error {
	if s.Dsn == "" {
		return errors.New("DSN must be supplied")
	}
	raven.SetDSN(s.Dsn)
	return nil
}

type Extra map[string]interface{}

// Send alert to sentry
func (s Service) Send(data alert.Data) error {

	// Sentry is only for errors
	if data.Level != alert.AlertError {
		return nil
	}

	packet := &raven.Packet{
		Level:   raven.ERROR,
		Message: fmt.Sprintf("%s - %s", data.Name, data.Reason),
		Extra:   Extra{"Message": data.Message},
	}

	_, ch := raven.Capture(packet, nil)
	if err := <-ch; err != nil {
		log.Errorln(err)
	}

	return nil
}
