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
    "fmt"
    "os"
    "os/signal"
    
    "github.com/xaevman/app"
    "github.com/xaevman/crash"
    "github.com/xaevman/ini"
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
    shutdownChan = make(chan bool, 0)
)

// BlockUntilShutdown does exactly what it sounds like, it blocks until
// the shutdown signal is received, then calls Shutdown.
func BlockUntilShutdown() {
    <-shutdownChan
    Shutdown()
}

// Shutdown handles shutting down the server process, closing open logs, 
// and terminating subprocesses.
func Shutdown() bool {
    Log.Close()
    
    err := app.DeletePidFile()
    if err != nil {
        Log.Error("%v\n", err)
        return false
    }
    
    return true
}

// StartSingleton intializes the server process, attempting to make sure
// that it is the only such process running.
func StartSingleton () bool {
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

// catchSigInt 
func catchSigInt() {
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt)
    go func(){
        select {
        case <-c:
            NotifyShutdown()
            shutdownChan<- true
        }
    }()
}

// module initialization
func init () { 
    catchSigInt()

    AppConfig = ini.New(fmt.Sprintf("%s/%s.ini", ConfigDir, app.GetName()))
    Log = NewSrvLog()
    Log.SetDebugLogsEnabled(false)

    FileCrashHandler = new(crash.FileHandler)
    FileCrashHandler.SetCrashDir(CrashDir)
    crash.AddHandler(FileCrashHandler)

    EmailCrashHandler = crash.NewEmailHandler()
    crash.AddHandler(EmailCrashHandler)

    initNet()
    
    Http.RegisterHandler("/data/handlers", OnPrivHandlerUri, PRIVATE_HANDLER)

    ini.Subscribe(AppConfig, onCfgChange)
}
