//  ---------------------------------------------------------------------------
//
//  uriHandlers.go
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
    "github.com/xaevman/crash"

    "encoding/json"
    "net/http"
    "time"
)

func OnNetInfoUri(resp http.ResponseWriter, req *http.Request) {
    handlers  := Http.GetNetInfo()
    data, err := json.MarshalIndent(&handlers, "", "    ")
    if err != nil {
        resp.WriteHeader(http.StatusInternalServerError)
        return
    }

    resp.Write(data)
}

func OnShutdownUri(resp http.ResponseWriter, req *http.Request) {
    resp.Write([]byte("Shutdown initiated\n"))

    Log.Info("Shutdown initiated via http request")

    go func() {
        defer crash.HandleAll()

        <-time.After(2 * time.Second)
        signalShutdown()
    }()
}

func OnCrashUri(resp http.ResponseWriter, req *http.Request) {
    resp.Write([]byte("Crash initiated\n"))

    Log.Info("Crash initiated via http request")

    go func() {
        defer crash.HandleAll()

        <-time.After(2 * time.Second)
        crashChan<- true
    }()
}
