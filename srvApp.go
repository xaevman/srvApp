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
    "net/http"
    "net/http/pprof"
    "os"
    "os/signal"
    "runtime"
    
    "github.com/xaevman/app"
    "github.com/xaevman/crash"
    "github.com/xaevman/ini"
)

// RunMode enum
const (
    CMDLINE = iota
    RUN_SVC
    INST_SVC
    UNINST_SVC
)

// AppConfig represents the application's configuration file.
var AppConfig *ini.IniCfg

// AppProcess is a reference to the current process.
var AppProcess *os.Process

// CrashDir is the relative path to the application's crash
// directory.
var CrashDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "crash")

// ConfigDir is the relative path to the application's config
// directory.
var ConfigDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "config")

// EmailCrashHandler is one of two default crash handlers for a srvApp.
// This handler sends crash report emails to configured addresses on
// application crashes.
var EmailCrashHandler *crash.EmailHandler

// FileCrashHandler is one of two default crash handlers for a srvApp.
// This handler writes out a JSON-formatted crash report file on
// application crashes.
var FileCrashHandler *crash.FileHandler

// Http is the http sever object for the application.
var Http *HttpSrv = NewHttpSrv()

// Log is a helper object which holds 3 separte instances of
// file-backed log objects; One each for debug, info, and error logs.
var Log *SrvLog

// LogDir is the relative path to the application's log directory.
var LogDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "log")

// Internal vars.
var (
    crashChan        = make(chan bool, 0)
    modeInstallSvc   = false
    modeRunSvc       = false
    modeUninstallSvc = false
    runMode          byte
    shutdownChan     = make(chan bool, 0)
    shuttingDown     = false
)


// Init initializes the server appplication. Initializations sets up signal
// handling, arg parsing, run mode, initializes a default config file, 
// file and email crash handlers, both private and public facing http server
// listeners, and some default debugging Uri handlers.
func Init() {
    Log = NewSrvLog()
    parseFlags()
}

// Run executes the application in whatever run mode is configured.
func Run() {
    run()
}

// blockUntilShutdown does exactly what it sounds like, it blocks until
// the shutdown signal is received, then calls Shutdown.
func blockUntilShutdown() {
    <-shutdownChan
    shutdown()
}

// catchCrash
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

// catchSigInt 
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

// parseFlags
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

// preStart
func preStart() {
    catchSigInt()

    AppConfig = ini.New(fmt.Sprintf("%s/%s.ini", ConfigDir, app.GetName()))
    Log.SetDebugLogsEnabled(false)

    FileCrashHandler = new(crash.FileHandler)
    FileCrashHandler.SetCrashDir(CrashDir)
    crash.AddHandler(FileCrashHandler)

    EmailCrashHandler = crash.NewEmailHandler()
    crash.AddHandler(EmailCrashHandler)

    initNet()
    catchCrash()
    
    Http.RegisterHandler("/cmd/crash/", OnCrashUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/cmd/shutdown/", OnShutdownUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/netinfo/", OnNetInfoUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/", http.HandlerFunc(pprof.Index), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/profile", http.HandlerFunc(pprof.Profile), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/trace", http.HandlerFunc(pprof.Trace), PRIVATE_HANDLER)

    ini.Subscribe(AppConfig, onCfgChange)
}

// setRunMode
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

    Log.Close()

    shuttingDown = true
    
    close(shutdownChan)
    close(crashChan)

    err := app.DeletePidFile()
    if err != nil {
        Log.Error("%v\n", err)
        return false
    }
    
    return true
}

// signalShutdown
func signalShutdown() {
    go func() {
        defer crash.HandleAll()
        shutdownChan<- true
    }()
}

// startSingleton intializes the server process, attempting to make sure
// that it is the only such process running.
func startSingleton() bool {
    preStart()

    AppProcess = app.GetRunStatus()
    if AppProcess != nil {
        Log.Error(
            "Application already running under PID %d\n", 
            AppProcess.Pid,
        )
        return false
    }
    
    return true
}
