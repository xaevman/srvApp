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
    "github.com/jeffail/gabs"
    "github.com/xaevman/crash"

    "encoding/json"
    "fmt"
    "io/ioutil"
    "mime"
    "net/http"
    "path/filepath"
    "time"
)

func OnAppInfoUri(resp http.ResponseWriter, req *http.Request) {
    handlers  := httpSrv.getNetInfo()
    data, err := json.MarshalIndent(&handlers, "", "    ")
    if err != nil {
        http.Error(
            resp, 
            fmt.Sprintf("%d : Internal Error", http.StatusInternalServerError),
            http.StatusInternalServerError,
        )
        return
    }

    resp.Write(data)
}

func OnCountersUri(resp http.ResponseWriter, req *http.Request) {
    
}

func OnCrashUri(resp http.ResponseWriter, req *http.Request) {
    resp.Write([]byte("Crash initiated\n"))

    srvLog.Info("Crash initiated via http request")

    go func() {
        defer crash.HandleAll()

        <-time.After(2 * time.Second)
        crashChan<- true
    }()
}

func OnLogsUri(resp http.ResponseWriter, req *http.Request) {
    logs := LogBuffer().ReadAll()
    json := gabs.New()
    json.Array("logs")

    for i := range logs {
        json.ArrayAppend(logs[i], "logs")
    }

    resp.Write(json.Bytes())
}

func OnPrivStaticSrvUri(resp http.ResponseWriter, req *http.Request) {
    srcDir := httpSrv.privStaticDir()
    if srcDir == "" {
        http.Error(
            resp, 
            fmt.Sprintf("%d : Not Found", http.StatusNotFound),
            http.StatusNotFound,
        )
        return
    }
    
    serveStaticFile(resp, req, srcDir)
}

func OnPubStaticSrvUri(resp http.ResponseWriter, req *http.Request) {
    srcDir := httpSrv.pubStaticDir()
    if srcDir == "" {
        http.Error(
            resp, 
            fmt.Sprintf("%d : Not Found", http.StatusNotFound),
            http.StatusNotFound,
        )
        return
    }

    serveStaticFile(resp, req, srcDir)
}

func OnShutdownUri(resp http.ResponseWriter, req *http.Request) {
    resp.Write([]byte("Shutdown initiated\n"))

    srvLog.Info("Shutdown initiated via http request")

    go func() {
        defer crash.HandleAll()

        <-time.After(2 * time.Second)
        signalShutdown()
    }()
}

func serveStaticFile(resp http.ResponseWriter, req *http.Request, srcDir string) {
    fName := req.URL.Path
    if fName == "" || fName == "/" {
        fName = "index.html"
    }

    fPath    := filepath.Join(srcDir, fName)
    mimeType := mime.TypeByExtension(filepath.Ext(fPath))

    data, err := ioutil.ReadFile(fPath)
    if err != nil {
        http.Error(
            resp,
            fmt.Sprintf("%d : %v", http.StatusNotFound, err),
            http.StatusNotFound,
        )
    }

    resp.Header().Set("Content-Type", mimeType)
    resp.Write(data)
}
