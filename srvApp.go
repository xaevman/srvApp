//  ---------------------------------------------------------------------------
//
//  srvApp.go
//
//  Copyright (c) 2015, Jared Chavez.
//  All rights reserved.
//
//  Use of this source code is governed by a BSD-style
//  license that can be found in the LICENSE file.
//
//  -----------

// Package srvApp manages common server tasks related to managing
// application state, logging, and configuration.
package srvApp

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"

	"github.com/xaevman/app"
	"github.com/xaevman/counters"
	"github.com/xaevman/crash"
	"github.com/xaevman/ini"
	"github.com/xaevman/log"
	"github.com/xaevman/shutdown"
	_ "github.com/xaevman/trace"
)

// RunMode enum
const (
	CMDLINE = iota
	RUN_SVC
	INST_SVC
	UNINST_SVC
)

// coutner names
const APP_UPTIME_COUNTER = "app.uptime_sec"

// RunCfg represents the desired run configuration for the application before
// ini or command line flag based options are taken into consideration. This can
// currently be used to force enable/disable the http listeners depending on run
// mode.
type RunCfg struct {
	InitSrvCmd bool
	InitSrvSvc bool
}

// AppConfig returns a reference to the application's configuration file.
func AppConfig() *ini.IniCfg {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return appConfig
}

var appConfig *ini.IniCfg

// AppCounters returns a reference to the container for this application's
// performance counters.
func AppCounters() *counters.List {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return appCounters
}

var appCounters = counters.NewList()

// AppProcess returns a reference to the current process.
func AppProcess() *os.Process {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return appProcess
}

var appProcess *os.Process

// CrashDir returns the relative path to the application's crash
// directory.
func CrashDir() string {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return crashDir
}

var crashDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "crash")

// ConfigDir returns the relative path to the application's config
// directory.
func ConfigDir() string {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return configDir
}

var configDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "config")

// EmailCrashHandler returns the email crash handler for the app.
func EmailCrashHandler() *crash.EmailHandler {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return emailCrashHandler
}

var emailCrashHandler *crash.EmailHandler

// FileCrashHandler returns the file crash handler for the app.
func FileCrashHandler() *crash.FileHandler {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return fileCrashHandler
}

var fileCrashHandler *crash.FileHandler

// Http returns a refernce to the http sever object for the application.
func Http() *HttpSrv {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return httpSrv
}

var httpSrv = NewHttpSrv()

// Log returns a refernce to the SrvLog instance for the application.
func Log() *SrvLog {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return srvLog
}

var srvLog *SrvLog

// internal log buffer object
func LogBuffer() *log.LogBuffer {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return logBuffer
}

var logBuffer *log.LogBuffer

// LogDir returns the relative path to the application's log directory.
func LogDir() string {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return logDir
}

var logDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "log")

// Internal vars.
var (
	cleanPid         = true
	crashChan        = make(chan bool, 0)
	exitCode         = 0
	modeInstallSvc   = false
	modeRunSvc       = false
	modeUninstallSvc = false
	cfgLock          sync.RWMutex
	runLock          sync.Mutex
	runMode          byte
	appShutdown      = shutdown.New()
	shuttingDown     = false
	runCfg           *RunCfg
)

// Init calls InitCfg to initialize the server appplication with a default configuration.
func Init() {
	InitCfg(&RunCfg{
		InitSrvCmd: true,
		InitSrvSvc: true,
	})
}

// InitCfg Initializations sets up signal
// handling, arg parsing, run mode, initializes a default config file,
// file and email crash handlers, both private and public facing http server
// listeners, and some default debugging Uri handlers.
func InitCfg(cfg *RunCfg) {
	runLock.Lock()
	defer runLock.Unlock()

	cfgLock.Lock()
	defer cfgLock.Unlock()

	runCfg = cfg

	os.Chdir(app.GetExeDir())

	parseFlags()
	initLogs()

	afterFlags()
	if shuttingDown {
		return
	}

	uptime := counters.NewTimer(APP_UPTIME_COUNTER)
	uptime.Set(0)
	appCounters.Add(uptime)

	appConfig = ini.New(fmt.Sprintf("%s/%s.ini", configDir, app.GetName()))

	fileCrashHandler = new(crash.FileHandler)
	fileCrashHandler.SetCrashDir(crashDir)
	crash.AddHandler(fileCrashHandler)

	emailCrashHandler = crash.NewEmailHandler()
	crash.AddHandler(emailCrashHandler)

	catchSigInt()
	catchCrash()
	netInit()

	ini.Subscribe(appConfig, onCfgChange)
}

// QueryRunmode returns the configured runmode for the application
func QueryRunmode() byte {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return runMode
}

// QueryShutdown returns a boolean values indicating whether or not
// the application is shutting down
func QueryShutdown() bool {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return shuttingDown
}

// Run executes the application in whatever run mode is configured.
func Run() int {
	runLock.Lock()
	defer runLock.Unlock()

	return run()
}

// SignalShutdown ensures a config lock before calling
// the unsafe _signalShutdown
func SignalShutdown(returnCode int) {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	_signalShutdown(returnCode)
}

// blockUntilShutdown does exactly what it sounds like, it blocks until
// the shutdown signal is received, then calls Shutdown.
func blockUntilShutdown() int {
	<-appShutdown.Signal

	_shutdown()

	appShutdown.Complete()
	if appShutdown.WaitForTimeout() {
		var buffer bytes.Buffer
		pprof.Lookup("goroutine").WriteTo(&buffer, 1)

		panic(fmt.Sprintf("Timeout shutting down srvApp\n\n%s", buffer.String()))
	}

	return exitCode
}

// catchCrash catches the user-initiated crash signal and happily panics
// the application.
func catchCrash() {
	go func() {
		defer crash.HandleAll()

		select {
		case <-crashChan:
			cfgLock.RLock()
			isShuttingDown := shuttingDown
			cfgLock.RUnlock()

			if isShuttingDown {
				return
			}

			panic("User initiated crash")
		}
	}()
}

// catchSigInt catches the SIGINT signal and, in turn, signals the applciation
// to start a graceful shutdown.
func catchSigInt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		defer crash.HandleAll()

		select {
		case <-c:
			go func() {
				defer crash.HandleAll()
				_signalShutdown(0)
			}()
		}
	}()
}

// parseFlags registers and parses default command line flags, then
// sets the default run mode for the application.
func parseFlags() {
	// windows-specific options
	if runtime.GOOS == "windows" {
		flag.BoolVar(
			&modeRunSvc,
			"runSvc",
			false,
			"Run the application as a windows service.",
		)

		flag.BoolVar(
			&modeInstallSvc,
			"install",
			false,
			"Install the application as a windows service.",
		)

		flag.BoolVar(
			&modeUninstallSvc,
			"uninstall",
			false,
			"Uninstall the application's service instance.",
		)
	}

	flag.Parse()

	setRunMode()
}

// setRunMode stores the run mode for the application.
func setRunMode() {
	if modeRunSvc {
		runMode = RUN_SVC
	} else if modeInstallSvc {
		runMode = INST_SVC
	} else if modeUninstallSvc {
		runMode = UNINST_SVC
	} else {
		runMode = CMDLINE
	}
}

// shutdown handles shutting down the server process, closing open logs,
// and terminating subprocesses.
func _shutdown() {
	notifyShutdown()

	netShutdown()
	ini.Shutdown()

	close(crashChan)

	if cleanPid {
		err := app.DeletePidFile()
		if err != nil {
			Log().Error("%v\n", err)
		}
	}

	closeLogs()
}

// _signalShutdown asynchronously signals the application to shutdown.
func _signalShutdown(returnCode int) {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	if shuttingDown {
		return
	}

	shuttingDown = true
	exitCode = returnCode

	appShutdown.Start()
}

// startSingleton intializes the server process, attempting to make sure
// that it is the only such process running.
func startSingleton() bool {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	appProcess = app.GetRunStatus()

	if appProcess != nil {
		srvLog.Error(
			"Application already running under PID %d\n",
			appProcess.Pid,
		)

		cleanPid = false
		return false
	}

	_, err := app.CreatePidFile()
	if err != nil {
		srvLog.Error("Error creating PID file (%s)", err)
		return false
	}

	appProcess = app.GetRunStatus()

	if appProcess == nil {
		srvLog.Error("Error finding our process")
		return false
	}

	return true
}
