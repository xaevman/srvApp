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
    "flag"
    "fmt"
    "os"
    "os/signal"
    "runtime"
    "sync"

    "github.com/xaevman/app"
    "github.com/xaevman/counters"
    "github.com/xaevman/crash"
    "github.com/xaevman/ini"
    "github.com/xaevman/log"
)

// RunMode enum
const (
    CMDLINE = iota
    RUN_SVC
    INST_SVC
    UNINST_SVC
)

// AppConfig returns a reference to the application's configuration file.
func AppConfig() *ini.IniCfg {
    return appConfig
}
var appConfig *ini.IniCfg

// AppCounters returns a reference to the container for this application's
// performance counters.
func AppCounters() *counters.List {
    return appCounters
}
var appCounters = counters.NewList()

// AppProcess returns a reference to the current process.
func AppProcess() *os.Process {
    return appProcess
}
var appProcess *os.Process

// CrashDir returns the relative path to the application's crash
// directory.
func CrashDir() string {
    return crashDir
}
var crashDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "crash")

// ConfigDir returns the relative path to the application's config
// directory.
func ConfigDir() string {
    return configDir
}
var configDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "config")

// EmailCrashHandler returns the email crash handler for the app.
func EmailCrashHandler() *crash.EmailHandler {
    return emailCrashHandler
}
var emailCrashHandler *crash.EmailHandler

// FileCrashHandler returns the file crash handler for the app.
func FileCrashHandler() *crash.FileHandler {
    return fileCrashHandler
}
var fileCrashHandler *crash.FileHandler

// Http returns a refernce to the http sever object for the application.
func Http() *HttpSrv {
    return httpSrv
}
var httpSrv = NewHttpSrv()

// Log returns a refernce to the SrvLog instance for the application.
func Log() *SrvLog {
    return srvLog
}
var srvLog *SrvLog

// internal log buffer object
func LogBuffer() *log.LogBuffer {
    return logBuffer
}
var logBuffer *log.LogBuffer

// LogDir returns the relative path to the application's log directory.
func LogDir() string {
    return logDir
}
var logDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "log")

// Internal vars.
var (
    crashChan        = make(chan bool, 0)
    modeInstallSvc   = false
    modeRunSvc       = false
    modeUninstallSvc = false
    runLock            sync.Mutex
    runMode            byte
    shutdownChan     = make(chan bool, 0)
    shuttingDown     = false
)

// Init initializes the server appplication. Initializations sets up signal
// handling, arg parsing, run mode, initializes a default config file, 
// file and email crash handlers, both private and public facing http server
// listeners, and some default debugging Uri handlers.
func Init() {
    runLock.Lock()
    defer runLock.Unlock()

    os.Chdir(app.GetExeDir())

    parseFlags()
    initLogs()

    afterFlags()
    if shuttingDown {
        return
    }

    uptime := counters.NewTimer("app.uptime_sec")
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
    initNet()

    ini.Subscribe(appConfig, onCfgChange)
}

// QueryShutdown returns a value
func QueryShutdown() bool {
    runLock.Lock()
    defer runLock.Unlock()

    return shuttingDown
}

// Run executes the application in whatever run mode is configured.
func Run() {
    runLock.Lock()
    defer runLock.Unlock()

    run()
}

// blockUntilShutdown does exactly what it sounds like, it blocks until
// the shutdown signal is received, then calls Shutdown.
func blockUntilShutdown() {
    <-shutdownChan
    shutdown()
}

// catchCrash catches the user-initiated crash signal and happily panics
// the application.
func catchCrash() {
    go func() {
        defer crash.HandleAll()

        select {
        case <-crashChan:
            if shuttingDown {
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
    go func(){
        defer crash.HandleAll()

        select {
        case <-c:
            signalShutdown()
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
func shutdown() bool {
    notifyShutdown()

    srvLog.Close()
    
    close(shutdownChan)
    close(crashChan)

    err := app.DeletePidFile()
    if err != nil {
        srvLog.Error("%v\n", err)
        return false
    }
    
    return true
}

// signalShutdown asynchronously signals the application to shutdown.
func signalShutdown() {
    shuttingDown = true

    go func() {
        defer crash.HandleAll()
        shutdownChan<- true
    }()
}

// startSingleton intializes the server process, attempting to make sure
// that it is the only such process running.
func startSingleton() bool {
    appProcess = app.GetRunStatus()
    if appProcess != nil {
        srvLog.Error(
            "Application already running under PID %d\n", 
            appProcess.Pid,
        )
        return false
    }
    
    return true
}
