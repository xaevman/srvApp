//  ---------------------------------------------------------------------------
//
//  log.go
//
//  Copyright (c) 2015, Jared Chavez. 
//  All rights reserved.
//
//  Use of this source code is governed by a BSD-style
//  license that can be found in the LICENSE file.
//
//  -----------

package srvApp

import (
    "github.com/xaevman/log"
    "github.com/xaevman/log/flog"

    "sync"
)

// SrvLog contains helper functions which distribute log messages
// between separate debug, info, and error log objects.
type SrvLog struct {
    lock sync.RWMutex
    subs map[string][]log.LogNotify
}

// NewSrvLog returns a new instance of a SrvLog object.
func NewSrvLog() *SrvLog {
    obj := &SrvLog {
        subs : make(map[string][]log.LogNotify),
    }

    obj.AddLog("debug", logBuffer)
    obj.AddLog("error", logBuffer)
    obj.AddLog("info",  logBuffer)

    obj.AddLog("debug", flog.New("debug", logDir, flog.BufferedFile))
    obj.AddLog("error", flog.New("error", logDir, flog.DirectFile))
    obj.AddLog("info",  flog.New("info",  logDir, flog.BufferedFile))

    return obj
}

// AddLog
func (this *SrvLog) AddLog(name string, newLog log.LogNotify) {
    this.lock.Lock()
    defer this.lock.Unlock()

    _, ok := this.subs[name]
    if !ok {
        this.subs[name] = make([]log.LogNotify, 0, 1)
    }

    this.subs[name] = append(this.subs[name], newLog)
}

// Close closes the debug, err, and info flog instances.
func (this *SrvLog) Close() {
    this.lock.RLock()
    defer this.lock.RUnlock()

    for k, _ := range this.subs {
        for i := range this.subs[k] {
            l, ok := this.subs[k][i].(log.LogCloser)
            if ok {
                l.Close()
            }
        }
    }
}

// Debug is a proxy which passes its arguments along to the underlying
// debug flog instance.
func (this *SrvLog) Debug(format string, v ...interface{}) {
    this.LogTo("debug", format, v...)
}

// Error is a proxy which passes its arguments along to the underlying
// error flog instance.
func (this *SrvLog) Error(format string, v ...interface{}) {
    this.LogTo("error", format, v...)
}

// Info is a proxy which passes its arguments along to the underlying
// info flog instance.
func (this *SrvLog) Info(format string, v ...interface{}) {
    this.LogTo("info", format, v...)
}

// LogTo logs to the registered loggers with the specified key, using
// the supplied formatting string and arguments.
func (this *SrvLog) LogTo(name, format string, v ...interface{}) {
    msg := log.NewLogMsg(name, format, 3, v...)

    this.lock.RLock()
    defer this.lock.RUnlock()

    logs, ok := this.subs[name]
    if !ok {
        srvLog.Error("Couldn't log to %s logs. Loggers missing.", name)
        return
    }

    for i := range logs {
        logs[i].Print(msg)
    }
}

// SetDebugFlushIntervalSec sets the flush interval for the debug log.
func (this *SrvLog) SetFlushIntervalSec(name string, interval int32) {
    this.lock.RLock()
    defer this.lock.RUnlock()

    logs, ok := this.subs[name]
    if !ok {
        srvLog.Error(
            "Couldn't change flush interval on %s logs. Loggers missing", 
            name,
        )
        return
    }
    
    for i := range logs {
        dbgLog, ok := logs[i].(*flog.BufferedLog)
        if ok {
            dbgLog.SetFlushIntervalSec(interval)
        }
    }
}

// SetDebugLogs enables or disables debug logging.
func (this *SrvLog) SetLogsEnabled(name string, val bool) {
    this.lock.RLock()
    defer this.lock.RUnlock()
    
    logs, ok := this.subs[name]
    if !ok {
        srvLog.Error(
            "Couldn't set enabled (%t) on %s logs. Loggers missing", 
            val,
            name,
        )
        return
    }

    for i := range logs {
        l, ok := logs[i].(log.LogToggler)
        if ok {
            l.SetEnabled(val)
        }
    }
}

// initLogs intializes the log buffer, console, and file-backed logging services
// for the application.
func initLogs() {
    logBuffer = log.NewLogBuffer(DefaultHttpLogBuffers)
    srvLog    = NewSrvLog()
}
