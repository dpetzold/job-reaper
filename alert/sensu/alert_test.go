package sensu

import (
	"testing"
	"time"

	"github.com/sstarcher/job-reaper/alert"
	"github.com/stretchr/testify/assert"
)

func TestSensu(t *testing.T) {
	data := alert.Data{
		Name:      "name",
		Message:   "message string of values",
		Status:    "Unknown",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		ExitCode:  1,
		Namespace: "default",
	}
	tmpl := "Hi {{ .Name }} {{ .Namespace }} End"
	value, _ := generateTemplate(tmpl, data)
	assert.Equal(t, value, "Hi name default End", "they should be equal")
}
