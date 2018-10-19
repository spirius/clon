package cmd

import (
	"bytes"

	"github.com/fatih/color"
	log "github.com/sirupsen/logrus"
)

type logFormatter struct{}

var logLevelColor = map[log.Level]struct{ key, value *color.Color }{
	log.PanicLevel: {color.New(color.FgRed), color.New(color.FgRed)},
	log.FatalLevel: {color.New(color.FgRed), color.New(color.FgRed)},
	log.ErrorLevel: {color.New(color.FgRed), color.New(color.FgRed)},
	log.WarnLevel:  {color.New(color.FgYellow), nil},
	log.InfoLevel:  {color.New(color.FgCyan), nil},
	log.DebugLevel: {color.New(color.FgWhite), nil},
}

func (l *logFormatter) Format(e *log.Entry) ([]byte, error) {
	var buf bytes.Buffer
	c := logLevelColor[e.Level]
	buf.WriteString(c.key.Sprint("info"))
	buf.WriteString(": ")
	if c.value != nil {
		buf.WriteString(c.value.Sprint(e.Message))
	} else {
		buf.WriteString(e.Message)
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}