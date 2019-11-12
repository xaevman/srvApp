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
	"fmt"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/xaevman/log"
	"github.com/xaevman/log/flog"
	"github.com/xaevman/trace"
)

// logShutdownChan is used to signal the log rotator goroutine
// to exit cleanly.
var logShutdownChan = make(chan chan interface{}, 0)
var logRotateChan = make(chan interface{}, 0)

// LogService represents a named collection of related LogNotify objects
// within a SrvLog and maintains the enabled flag for that named log.
type LogService struct {
	enabled   bool
	notifiers []log.LogNotify
}

// SrvLog contains helper functions which distribute log messages
// between separate debug, info, and error log objects.
type SrvLog struct {
	lock   sync.RWMutex
	subs   map[string]*LogService
	fields map[string]interface{}
}

// NewSrvLog returns a new instance of a SrvLog object.
func NewSrvLog() *SrvLog {
	obj := &SrvLog{
		subs:   make(map[string]*LogService),
		fields: make(map[string]interface{}),
	}

	obj.AddLog("debug", logBuffer)
	obj.AddLog("error", logBuffer)
	obj.AddLog("info", logBuffer)

	fileLog := flog.New("all", logDir, flog.BufferedFile)

	obj.AddLog("debug", fileLog)
	obj.AddLog("error", fileLog)
	obj.AddLog("info", fileLog)

	return obj
}

// AddLog
func (this *SrvLog) AddLog(name string, newLog log.LogNotify) {
	this.lock.Lock()
	defer this.lock.Unlock()

	logger, exists := this.subs[name]
	if !exists {
		logger = &LogService{
			enabled:   true,
			notifiers: make([]log.LogNotify, 0, 1),
		}

		this.subs[name] = logger
	}

	logger.notifiers = append(logger.notifiers, newLog)
}

// Close closes the debug, err, and info flog instances.
func (this *SrvLog) Close() {
	this.lock.RLock()
	defer this.lock.RUnlock()

	for k := range this.subs {
		for i := range this.subs[k].notifiers {
			l, isLogCloser := this.subs[k].notifiers[i].(log.LogCloser)
			if isLogCloser {
				l.Close()
			}
		}
	}
}

// FileLogBufferSizeB returns the current capacity of the underlying memory
// buffer set aside for the file log
func (this *SrvLog) FileLogBufferCap() int {
	this.lock.RLock()
	defer this.lock.RUnlock()

	totalCap := 0

	logBuffers := make(map[*flog.BufferedLog]interface{})
	for k := range this.subs {
		for i := range this.subs[k].notifiers {
			l, isBufferedLog := this.subs[k].notifiers[i].(*flog.BufferedLog)
			if isBufferedLog {
				_, exists := logBuffers[l]
				if exists {
					continue
				}

				logBuffers[l] = nil
				totalCap += l.BufferCap()
			}
		}
	}

	return totalCap
}

// Debug is a proxy which passes its arguments along to the underlying
// debug flog instance.
func (this *SrvLog) Debug(format string, v ...interface{}) {
	this.LogTo(false, "debug", format, v...)
}

// Error is a proxy which passes its arguments along to the underlying
// error flog instance.
func (this *SrvLog) Error(format string, v ...interface{}) {
	this.LogTo(false, "error", format, v...)
}

// Info is a proxy which passes its arguments along to the underlying
// info flog instance.
func (this *SrvLog) Info(format string, v ...interface{}) {
	this.LogTo(false, "info", format, v...)
}

// Debug is a proxy which passes its arguments along to the underlying
// debug flog instance.
func (this *SrvLog) DebugLocal(format string, v ...interface{}) {
	this.LogTo(true, "debug", format, v...)
}

// Error is a proxy which passes its arguments along to the underlying
// error flog instance.
func (this *SrvLog) ErrorLocal(format string, v ...interface{}) {
	this.LogTo(true, "error", format, v...)
}

// Info is a proxy which passes its arguments along to the underlying
// info flog instance.
func (this *SrvLog) InfoLocal(format string, v ...interface{}) {
	this.LogTo(true, "info", format, v...)
}

// LogTo logs to the registered loggers with the specified key, using
// the supplied formatting string and arguments.
func (this *SrvLog) LogTo(local bool, name, format string, v ...interface{}) {
	msg := log.NewLogMsg(name, format, 3, v...)

	this.lock.RLock()
	defer this.lock.RUnlock()

	logs, exists := this.subs[name]
	if !exists {
		srvLog.Error("Couldn't log to %s logs. Loggers missing.", name)
		return
	}

	if !logs.enabled {
		return
	}

	// append fields to the message
	fieldCount := len(this.fields)
	if fieldCount > 0 {
		keys := make([]string, 0, fieldCount)
		for key := range this.fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for index, key := range keys {
			if index > 0 {
				msg.Message += ","
			}
			msg.Message += fmt.Sprintf(" %s=%+v", key, this.fields[key])
		}
	}

	for i := range logs.notifiers {
		logs.notifiers[i].Print(msg)
	}
}

// SetDebugFlushIntervalSec sets the flush interval for the named log.
func (this *SrvLog) SetFlushIntervalSec(name string, interval int32) {
	this.lock.RLock()
	defer this.lock.RUnlock()

	logs, exists := this.subs[name]
	if !exists {
		srvLog.Error(
			"Couldn't change flush interval on %s logs. Loggers missing",
			name,
		)
		return
	}

	for i := range logs.notifiers {
		dbgLog, isBuffered := logs.notifiers[i].(*flog.BufferedLog)
		if isBuffered {
			dbgLog.SetFlushIntervalSec(interval)
		}
	}
}

// SetDebugLogs enables or disables named logs.
func (this *SrvLog) SetLogsEnabled(name string, val bool) {
	this.lock.RLock()
	defer this.lock.RUnlock()

	logs, exists := this.subs[name]
	if !exists {
		srvLog.Error(
			"Couldn't set enabled (%t) on %s logs. Loggers missing",
			val,
			name,
		)
		return
	}

	logs.enabled = val
}

// WithField stores a key, value pair in the logger that will be included in all subsquent logs.
func (this *SrvLog) WithField(key string, value interface{}) *SrvLog {
	return this.WithFields(map[string]interface{}{
		key: value,
	})
}

// WithFields stores all the given key, value pairs in the logger that will be includeded in all subsequent logs.
func (this *SrvLog) WithFields(fields map[string]interface{}) *SrvLog {
	this.lock.RLock()
	defer this.lock.RUnlock()

	newSubs := make(map[string]*LogService)
	for key, val := range this.subs {
		newSubs[key] = val
	}

	newFields := make(map[string]interface{})
	for key, val := range this.fields {
		newFields[key] = val
	}
	for key, val := range fields {
		newFields[key] = val
	}

	derived := &SrvLog{
		subs:   newSubs,
		fields: newFields,
	}
	return derived
}

// closeLogs signals the log loop to cleanly close log files and exit
func closeLogs() {
	shutdownComplete := make(chan interface{}, 0)
	logShutdownChan <- shutdownComplete
	<-shutdownComplete
}

// initLogs runs a continuous loop, handling log initialization on startup,
// and then rotating logs once per day. The loop is broken once a shutdown
// signal is received on the logShutdownChan channel.
func initLogs() {
	newLog()
	go func() {
		for {
			select {
			case shutdownComplete := <-logShutdownChan:
				cycleLog()
				shutdownComplete <- nil
				return
			case <-time.After(24 * time.Hour):
				cycleLog()
				newLog()
			case <-logRotateChan:
				cycleLog()
				newLog()
			}
		}
	}()
}

// newLog intializes the log buffer, console, and file-backed logging services
// for the application.
func newLog() {
	logBuffer = log.NewLogBuffer(DefaultHttpLogBuffers)
	srvLog = NewSrvLog()

	trace.DebugLogger = srvLog
	trace.ErrorLogger = srvLog
}

// cycleLog closes the log file and renames it to include a UTC unix timestamp
func cycleLog() {
	srvLog.Info("Log rotate")
	srvLog.Close()

	srcPath := path.Join(logDir, "all.log")
	dstPath := fmt.Sprintf("%s.%d", srcPath, time.Now().UTC().Unix())
	os.Rename(srcPath, dstPath)
}
