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
    "github.com/xaevman/counters"
    "github.com/xaevman/crash"

    "encoding/json"
    "fmt"
    "io/ioutil"
    "mime"
    "net/http"
    "path/filepath"
    "time"
)

// OnAppInfoUri handles requests to the /debug/appinfo/ uri.
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

// OnCountersUri handles requests to the /debug/counters/ uri.
func OnCountersUri(resp http.ResponseWriter, req *http.Request) {
    cList := make(map[string]interface{})

    AppCounters().Do(func(c counters.Counter) error {
        cList[c.Name()] = c.GetRaw()
        return nil
    })

    data, err := json.MarshalIndent(&cList, "", "    ")
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

// OnCrashUri handles requests to the /cmd/crash/ uri.
func OnCrashUri(resp http.ResponseWriter, req *http.Request) {
    if req.Method != "POST" {
        http.Error(
            resp, 
            fmt.Sprintf("%d : Method not supported", http.StatusMethodNotAllowed),
            http.StatusMethodNotAllowed,
        )
        return
    }

    resp.Write([]byte("Crash initiated\n"))

    srvLog.Info("Crash initiated via http request")

    go func() {
        defer crash.HandleAll()

        <-time.After(2 * time.Second)
        crashChan<- true
    }()
}

// OnLogsUri handles requests to the /debug/logs/ uri.
func OnLogsUri(resp http.ResponseWriter, req *http.Request) {
    logs := LogBuffer().ReadAll()

    data, err := json.MarshalIndent(&logs, "", "    ")
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

// OnPrivStaticSrvUri handles static file requests on the private side
// interfaces.
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

// OnPubStaticSrvUri handles static file requests on the public side
// interfaces.
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

// OnShutdownUri handles requests on the /cmd/shutdown/ uri.
func OnShutdownUri(resp http.ResponseWriter, req *http.Request) {
    if req.Method != "POST" {
        http.Error(
            resp, 
            fmt.Sprintf("%d : Method not supported", http.StatusMethodNotAllowed),
            http.StatusMethodNotAllowed,
        )
        return
    }

    resp.Write([]byte("Shutdown initiated\n"))

    srvLog.Info("Shutdown initiated via http request")

    go func() {
        defer crash.HandleAll()

        <-time.After(2 * time.Second)
        signalShutdown()
    }()
}

// serveStaticFile is a helper function used by OnPrivStaticSrvUri and OnPubStaticSrvUri
// to serve static files from a given local directory on disk.
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
