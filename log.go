package cluster

import (
	"fmt"
	"log"
)

// Logger is for logging to a writer. This is not the raft replication log.
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
	Infof(format string, v ...interface{})
	Debugf(format string, v ...interface{})
	Tracef(format string, v ...interface{})
	Errorf(format string, v ...interface{})
	Errorln(v ...interface{})
}

// DiscardLogger is a noop logger.
type DiscardLogger struct {
}

// Println noop
func (d *DiscardLogger) Println(v ...interface{}) {}

// Printf noop
func (d *DiscardLogger) Printf(format string, v ...interface{}) {}

// Infof noop
func (d *DiscardLogger) Infof(format string, v ...interface{}) {}

// Debugf noop
func (d *DiscardLogger) Debugf(format string, v ...interface{}) {}

// Tracef noop
func (d *DiscardLogger) Tracef(format string, v ...interface{}) {}

// Errorln noop
func (d *DiscardLogger) Errorln(v ...interface{}) {}

// Errorf noop
func (d *DiscardLogger) Errorf(format string, v ...interface{}) {}

// LogLogger uses the std lib logger.
type LogLogger struct {
	Logger *log.Logger
}

// Println std lib
func (d *LogLogger) Println(v ...interface{}) {
	d.Logger.Output(3, "[INFO] "+fmt.Sprintln(v...))
}

// Printf std lib
func (d *LogLogger) Printf(format string, v ...interface{}) {
	d.Logger.Output(3, fmt.Sprintf("[INFO] "+format, v...))
}

// Infof std lib
func (d *LogLogger) Infof(format string, v ...interface{}) {
	d.Logger.Output(3, fmt.Sprintf("[INFO] "+format, v...))
}

// Debugf std lib
func (d *LogLogger) Debugf(format string, v ...interface{}) {
	d.Logger.Output(3, fmt.Sprintf("[DEBU] "+format, v...))
}

// Debugf std lib
func (d *LogLogger) Tracef(format string, v ...interface{}) {
	d.Logger.Output(3, fmt.Sprintf("[TRAC] "+format, v...))
}

// Errorln std lib
func (d *LogLogger) Errorln(v ...interface{}) {
	d.Logger.Output(3, "[ERRO] "+fmt.Sprintln(v...))
}

// Errorf std lib
func (d *LogLogger) Errorf(format string, v ...interface{}) {
	d.Logger.Output(3, fmt.Sprintf("[ERRO] "+format, v...))
}
