package srvApp

import (
    "github.com/xaevman/flog"
)

// SrvLog contains helper functions which distribute log messages
// between separate debug, info, and error log objects.
type SrvLog struct {
    dLog flog.FLog
    eLog flog.FLog
    iLog flog.FLog
}

// NewSrvLog returns a new instance of a SrvLog object.
func NewSrvLog () *SrvLog {
    obj := &SrvLog {
        dLog : flog.New("debug", LogDir, flog.BufferedFile),
        eLog : flog.New("error", LogDir, flog.DirectFile),
        iLog : flog.New("info",  LogDir, flog.BufferedFile),
    }
    
    return obj
}

// Close closes the debug, err, and info flog instances.
func (sl *SrvLog) Close () {
    sl.dLog.Close()
    sl.eLog.Close()
    sl.iLog.Close()
}

// Debug is a proxy which passes its arguments along to the underlying
// debug flog instance.
func (sl *SrvLog) Debug (format string, v ...interface{}) {
    sl.dLog.Print(format, v...)
}

// Error is a proxy which passes its arguments along to the underlying
// error flog instance.
func (sl *SrvLog) Error (format string, v ...interface{}) {
    sl.eLog.Print(format, v...)
}

// Info is a proxy which passes its arguments along to the underlying
// info flog instance.
func (sl *SrvLog) Info (format string, v ...interface{}) {
    sl.iLog.Print(format, v...)
}
