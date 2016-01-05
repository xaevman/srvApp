//  ---------------------------------------------------------------------------
//
//  run_win.go
//
//  Copyright (c) 2015, Jared Chavez. 
//  All rights reserved.
//
//  Use of this source code is governed by a BSD-style
//  license that can be found in the LICENSE file.
//
//  -----------

// +build windows

package srvApp

import (
    "golang.org/x/sys/windows/svc"
    "golang.org/x/sys/windows/svc/mgr"
    "github.com/xaevman/app"
)

const accept_cmds = svc.AcceptStop | svc.AcceptShutdown

type appSvc struct {}
func (this *appSvc) Execute(
    args    []string, 
    r       <-chan svc.ChangeRequest, 
    changes chan<- svc.Status,
) (
    ssec  bool, 
    errno uint32,
) {
    Log.Info("Initializing service")

    changes<- svc.Status {
        State : svc.StartPending,
    }

    if !startSingleton() {
        Log.Error("Failed to startSingleton")
        changes<- svc.Status {
            State : svc.StopPending,
        }

        signalShutdown()
    }

    Log.Info("Service initialized")
    changes<- svc.Status {
        Accepts : accept_cmds,
        State   : svc.Running,
    }

    for !shuttingDown {
        select {
        case <-shutdownChan:
            Log.Debug("Service shutdown signal received")
            shutdown()
        case c := <-r:
            switch c.Cmd {
            case svc.Interrogate:
                Log.Debug("Service interrogate: %v", c)
                changes<- c.CurrentStatus
            case svc.Stop, svc.Shutdown:
                Log.Info("Service stop requested")
                changes<- svc.Status {
                    State : svc.StopPending,
                }
                signalShutdown()
            default:
                Log.Error(
                    "Unhandled signal received from SCM: %v", 
                    c,
                )
            }
        }
    }

    changes<- svc.Status {
        State : svc.Stopped,
    }

    return
}

func installSvc() {
    Log.Info("Installing service %s", app.GetName())
    scm, err := mgr.Connect()
    if err != nil {
        Log.Error("Error connecting to SCM: %v", err)
        signalShutdown()
        return
    }

    defer scm.Disconnect()

    svc, err := scm.OpenService(app.GetName())
    if err == nil {
        Log.Error("Service already exists")
        signalShutdown()
        return
    }

    svc, err = scm.CreateService(
        app.GetName(),
        app.GetExePath(),
        mgr.Config{
            StartType : mgr.StartAutomatic,
        },
        "-runSvc",
    )
    defer svc.Close()

    if err != nil {
        Log.Error("Error creating service: %v", err)
        signalShutdown()
        return
    }

    Log.Info("Service %s installed", app.GetName())
}

func run() {
    switch runMode {
    case CMDLINE:
        runCmdline()
    case RUN_SVC:
        runSvc()
    case INST_SVC:
        installSvc()
    case UNINST_SVC:
        uninstallSvc()
    }
}

func runCmdline() {
    if !startSingleton() {
        signalShutdown()
    }

    blockUntilShutdown()
}

func runSvc() {
    err := svc.Run(app.GetName(), &appSvc{})
    if err != nil {
        Log.Error("Service execution error: %v", err)
        signalShutdown()
    }
}

func uninstallSvc() {
    Log.Info("Removing service %s", app.GetName())
    scm, err := mgr.Connect()
    if err != nil {
        Log.Error("Error connecting to SCM: %v", err)
        signalShutdown()
        return
    }

    defer scm.Disconnect()

    svc, err := scm.OpenService(app.GetName())
    if err != nil {
        Log.Error("Service %s doesn't exist", app.GetName())
        signalShutdown()
        return
    }

    defer svc.Close()
    err = svc.Delete()
    if err != nil {
        Log.Error("Error deleting service: %v", err)
        signalShutdown()
        return
    }

    Log.Info("Service %s deleted", app.GetName())
}
