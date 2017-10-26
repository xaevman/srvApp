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
	"github.com/xaevman/app"
	"github.com/xaevman/crash"
	"github.com/xaevman/log"

	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type AppInfo struct {
	Name    string
	NetInfo *NetInfo
}

func getAppInfo() *AppInfo {
	return &AppInfo{
		Name:    app.GetName(),
		NetInfo: httpSrv.getNetInfo(),
	}
}

func getAppInfoJSON() ([]byte, error) {
	return json.MarshalIndent(getAppInfo(), "", "    ")
}

// OnAppInfoUri handles requests to the /debug/appinfo/ uri.
func OnAppInfoUri(resp http.ResponseWriter, req *http.Request) {
	data, err := getAppInfoJSON()
	if err != nil {
		srvLog.Error("OnAppInfoUri :: %v", err)
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
func getCountersJSON() ([]byte, error) {
	return json.MarshalIndent(AppCounters(), "", "    ")
}

func OnCountersUri(resp http.ResponseWriter, req *http.Request) {
	data, err := getCountersJSON()
	if err != nil {
		srvLog.Error("OnCountersUri :: %v", err)
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
		crashChan <- true
	}()
}

// OnLogsUri handles requests to the /debug/logs/ uri.
func getLogBuffer() []*log.LogMsg {
	return logBuffer.ReadAll()
}

func getLogsJSON() ([]byte, error) {
	return json.MarshalIndent(logBuffer, "", "    ")
}

func OnLogsUri(resp http.ResponseWriter, req *http.Request) {
	data, err := getLogsJSON()
	if err != nil {
		srvLog.Error("OnLogsUri :: %v", err)
		http.Error(
			resp,
			fmt.Sprintf("%d : Internal Error", http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	resp.Write(data)
}

// OnPingUri handles requests to the /debug/ping/ uri.
func OnPingUri(resp http.ResponseWriter, req *http.Request) {
	srvLog.Debug("Ping request from %s", req.RemoteAddr)
	c, err := resp.Write([]byte("200 - OK"))
	if err != nil {
		srvLog.Error(
			"Error writing response after %d bytes (%v)",
			c,
			err,
		)
	}
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
		SignalShutdown(0)
	}()
}

// serveStaticFile is a helper function used by OnPrivStaticSrvUri and OnPubStaticSrvUri
// to serve static files from a given local directory on disk.
func serveStaticFile(resp http.ResponseWriter, req *http.Request, srcDir string) {
	fName := req.URL.Path
	if fName == "" || fName[len(fName)-1] == '/' {
		fName += "index.html"
	}

	fPath := filepath.Join(srcDir, fName)
	absPath, err := filepath.Abs(fPath)
	if err != nil {
		http.Error(
			resp,
			fmt.Sprintf("%d : %v", http.StatusNotFound, err),
			http.StatusNotFound,
		)
		return
	}

	pInfo, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		srvLog.Debug("Can't retrieve file %s", absPath)
		http.Error(
			resp,
			fmt.Sprintf("%d : File not found (%s)", http.StatusNotFound, fPath),
			http.StatusNotFound,
		)
		return
	} else if err != nil {
		http.Error(
			resp,
			fmt.Sprintf("%d : %v", http.StatusNotFound, err),
			http.StatusNotFound,
		)
		return
	}

	if pInfo.IsDir() {
		http.Error(
			resp,
			fmt.Sprintf("%d : File not found (%s)", http.StatusNotFound, fPath),
			http.StatusNotFound,
		)
		return
	}

	mimeType := mime.TypeByExtension(filepath.Ext(absPath))

	f, err := os.Open(absPath)
	if err != nil {
		http.Error(
			resp,
			fmt.Sprintf("%d : %v", http.StatusNotFound, err),
			http.StatusNotFound,
		)
		return
	}

	resp.Header().Set("Content-Type", mimeType)

	w := bufio.NewWriterSize(resp, 64*1024)
	io.Copy(w, f)
	w.Flush()
}
