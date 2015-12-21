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
    
    "github.com/xaevman/app"
    "github.com/xaevman/ini"
)

// ConfigDir is the relative path to the application's config
// directory.
var ConfigDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "config")

// LogDir is the relative path to the application's log directory.
var LogDir = fmt.Sprintf("%s/%s", app.GetExeDir(), "log")

// AppConfig represents the application's configuration file.
var AppConfig *ini.IniCfg

// AppProcess is a reference to the current process.
var AppProcess *os.Process

// Log is a helper object which holds 3 separte instances of
// file-backed log objects; One each for debug, info, and error logs.
var Log *SrvLog

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

// RunSingleton intializes the server process, attempting to make sure
// that it is the only such process running.
func RunSingleton () bool {
    AppProcess = app.GetRunStatus()
    if AppProcess != nil {
        Log.Error(
            "Application already running under PID %d, exiting...\n", 
            AppProcess.Pid,
        )
        return false
    }
    
    return true
}

// module initialization
func init () { 
    AppConfig = ini.New(fmt.Sprintf("%s/%s.ini", ConfigDir, app.GetName()))
    Log = NewSrvLog()
}
