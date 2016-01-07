//  ---------------------------------------------------------------------------
//
//  net.go
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

    "bytes"
    "fmt"
    "net"
    "net/http"
    "net/http/pprof"
    "sort"
    "sync"
    "sync/atomic"
)

// HandlerType enumeration.
const (
    PRIVATE_HANDLER   = iota
    PUBLIC_HANDLER
    ALL_HANDLER
)

// A list of private, reserved, network segments.
var PrivateNets []*net.IPNet

// A list of addresses local to the machine the application is running on.
var LocalAddrs []*net.IP

type srvAppListener struct {
    listener *net.TCPListener
    closing  int32
}

func (this *srvAppListener) close() {
    atomic.StoreInt32(&this.closing, 1)
    err := this.listener.Close()
    if err != nil {
        Log.Error("%v", err)
    }
}

func (this *srvAppListener) isClosing() bool {
    val := atomic.LoadInt32(&this.closing)
    return val == 1
}

func (this *srvAppListener) listen(addr string) error {
    ln, err := net.Listen("tcp", addr)
    if err != nil {
        return err
    }

    this.listener = ln.(*net.TCPListener)

    return nil
}

type HttpSrv struct {
    privateEnabled   bool
    privatePort      int
    privateHandlers  map[string]func(http.ResponseWriter, *http.Request)
    privateListeners map[string]*srvAppListener
    privateMux       *http.ServeMux
    privateSrv       *http.Server
    privateStaticDir string
    configLock       sync.RWMutex
    publicEnabled    bool
    publicPort       int
    publicHandlers   map[string]func(http.ResponseWriter, *http.Request)
    publicListeners  map[string]*srvAppListener
    publicMux        *http.ServeMux
    publicSrv        *http.Server
    publicStaticDir  string
}

func (this *HttpSrv) Configure(
    privateEnabled   bool,
    privatePort      int,
    privateStaticDir string,
    publicEnabled    bool,
    publicPort       int,
    publicStaticDir  string,
    forceRestart     bool,
) {
    this.configLock.Lock()
    defer this.configLock.Unlock()

    privateChanged := false
    publicChanged  := false

    if this.privateEnabled != privateEnabled {
        this.privateEnabled = privateEnabled
        privateChanged = true
    }

    if this.privatePort != privatePort {
        this.privatePort = privatePort
        privateChanged = true
    }

    if this.privateStaticDir != privateStaticDir {
        this.privateStaticDir = privateStaticDir
    }
    
    if this.publicEnabled != publicEnabled {
        this.publicEnabled = publicEnabled
        publicChanged = true
    }

    if this.publicPort != publicPort {
        this.publicPort = publicPort
        publicChanged = true
    }

    if this.publicStaticDir != publicStaticDir {
        this.publicStaticDir = publicStaticDir
    }

    if privateChanged || forceRestart {
        this.restartPrivateHttp()
    }

    if publicChanged || forceRestart {
        this.restartPublicHttp()
    }
}

func (this *HttpSrv) getNetInfo() map[string]map[string][]string {
    this.configLock.RLock()
    defer this.configLock.RUnlock()

    privHandlers := make([]string, 0, len(this.privateHandlers))
    privServers  := make([]string, 0, len(this.privateListeners))
    pubHandlers  := make([]string, 0, len(this.publicHandlers))
    pubServers   := make([]string, 0, len(this.publicListeners))

    for k, _ := range this.privateHandlers {
        privHandlers = append(privHandlers, k)
    }
    sort.Strings(privHandlers)

    for k, _ := range this.privateListeners {
        privServers = append(privServers, k)
    }
    sort.Strings(privServers)

    for k, _ := range this.publicHandlers {
        pubHandlers = append(pubHandlers, k)
    }
    sort.Strings(pubHandlers)

    for k, _ := range this.publicListeners {
        pubServers = append(pubServers, k)
    }
    sort.Strings(pubServers)

    handlers           := make(map[string]map[string][]string)
    handlers["private"] = make(map[string][]string)
    handlers["public"]  = make(map[string][]string)

    handlers["private"]["servers"]  = privServers
    handlers["private"]["handlers"] = privHandlers
    handlers["public"]["servers"]   = pubServers
    handlers["public"]["handlers"]  = pubHandlers

    return handlers
}

func (this *HttpSrv) privStaticDir() string {
    this.configLock.RLock()
    defer this.configLock.RUnlock()

    return this.privateStaticDir
}

func (this *HttpSrv) pubStaticDir() string {
    this.configLock.RLock()
    defer this.configLock.RUnlock()

    return this.publicStaticDir
}

func (this *HttpSrv) restartPrivateHttp() {
    addrList := make([]string, 0)

    // grab private network addresses
    for x := range LocalAddrs {
        local := false
        for y := range PrivateNets {
            ip := net.ParseIP(LocalAddrs[x].String())
            if PrivateNets[y].Contains(ip) {
                local = true
                break
            }
        }
        if local {
            addr := net.JoinHostPort(
                LocalAddrs[x].String(), 
                fmt.Sprintf("%d", this.privatePort),
            )
            addrList = append(addrList, addr)
        }
    }

    // shut down old listeners
    for k, _ := range this.privateListeners {
        this.privateListeners[k].close()
        delete(this.privateListeners, k)
    }

    // start up new listeners
    if !this.privateEnabled {
        return
    }

    for i := range addrList {
        ln  := &srvAppListener{}
        err := ln.listen(addrList[i])
        if err != nil {
            Log.Error("%v", err)
            continue
        }

        this.privateListeners[addrList[i]] = ln
                
        Log.Debug("Initializing PrivateHttp %s", addrList[i])
        go func(ln *srvAppListener) {
            defer crash.HandleAll()

            err := this.privateSrv.Serve(ln.listener)
            if ln.isClosing() {
                return
            }

            if err != nil {
                Log.Error("%v", err)
            }
        }(ln)
    }
}

func (this *HttpSrv) restartPublicHttp() {
    addrList  := make([]string, 0)

    // grab private network addresses
    for x := range LocalAddrs {
        local := false

        for y := range PrivateNets {
            ip := net.ParseIP(LocalAddrs[x].String())
            if PrivateNets[y].Contains(ip) {
                local = true
                break
            }
        }

        if !local {
            addr := net.JoinHostPort(
                LocalAddrs[x].String(), 
                fmt.Sprintf("%d", this.publicPort),
            )
            addrList = append(addrList, addr)
        }
    }

    // shut down old listeners
    for k, _ := range this.publicListeners {
        this.publicListeners[k].close()
        delete(this.publicListeners, k)
    }

    // start up new listeners
    if !this.publicEnabled {
        return
    }

    for i := range addrList {
        ln  := &srvAppListener{}
        err := ln.listen(addrList[i])
        if err != nil {
            Log.Error("%v", err)
            continue
        }

        this.publicListeners[addrList[i]] = ln
                
        Log.Debug("Initializing PublicHttp %s", addrList[i])
        go func(ln *srvAppListener) {
            defer crash.HandleAll()

            err := this.publicSrv.Serve(ln.listener)
            if ln.isClosing() {
                return
            }

            if err != nil {
                Log.Error("%v", err)
            }
        }(ln)
    }
}

func (this *HttpSrv) RegisterHandler(
    path string,
    f func(http.ResponseWriter, *http.Request),
    handlerType byte,
) {
    this.configLock.Lock()
    defer this.configLock.Unlock()

    switch handlerType {
    case PRIVATE_HANDLER:
        this.privateHandlers[path] = f
        this.privateMux.HandleFunc(path, f)
        Log.Info("Private HttpHandler %s registered", path)
    case PUBLIC_HANDLER:
        this.publicHandlers[path] = f
        this.publicMux.HandleFunc(path, f)
        Log.Info("Public HttpHandler %s registered", path)
    case ALL_HANDLER:
        this.privateHandlers[path] = f
        this.privateMux.HandleFunc(path, f)
        Log.Info("Private HttpHandler %s registered", path)
        this.publicHandlers[path] = f
        this.publicMux.HandleFunc(path, f)
        Log.Info("Public HttpHandler %s registered", path)
    default:
        Log.Error("Unknown handler type (%d)", handlerType)
    }
}

func ValidateRequestBody(
    resp   http.ResponseWriter, 
    req    *http.Request, 
    buffer *bytes.Buffer,
) error {
    if req.ContentLength < 1 {
        return fmt.Errorf("Zero length body")
    }

    bodyLen, err := buffer.ReadFrom(req.Body)
    if err != nil {
        return err
    }

    if bodyLen != req.ContentLength {
        return fmt.Errorf(
            "Content length mismatch (%d != %d)", 
            bodyLen, 
            req.ContentLength,
        )
    }

    return nil
}

func NewHttpSrv() *HttpSrv {
    newprivateMux := http.NewServeMux()
    newPublicMux  := http.NewServeMux()

    newSrv := &HttpSrv {
        privateEnabled    : DefaultPrivateHttpEnabled,
        privatePort       : DefaultPrivateHttpPort,
        privateHandlers   : make(map[string]func(http.ResponseWriter, *http.Request)),
        privateListeners  : make(map[string]*srvAppListener),
        privateMux        : newprivateMux,
        privateSrv        : &http.Server {
            Handler : newprivateMux,
        },
        publicEnabled   : DefaultPublicHttpEnabled,
        publicPort      : DefaultPublicHttpPort,
        publicHandlers  : make(map[string]func(http.ResponseWriter, *http.Request)),
        publicListeners : make(map[string]*srvAppListener),
        publicMux       : newPublicMux,
        publicSrv       : &http.Server {
            Handler : newPublicMux,
        },
    }

    return newSrv
}

func initNet() {
    // populate list of local addresses
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        panic(err)
    }

    LocalAddrs = make([]*net.IP, 0)
    for i := range addrs {
        ip, _, err := net.ParseCIDR(addrs[i].String())
        if err != nil {
            panic(err)
        }

        if ip.To4() == nil {
            continue
        }

        LocalAddrs = append(LocalAddrs, &ip)
    }

    Log.Info("LocalAddresses: %v", LocalAddrs)

    // populate list of private address networks
    PrivateNets = make([]*net.IPNet, 0)

    _, n1, err := net.ParseCIDR("10.0.0.0/8")
    if err != nil {
        panic(err)
    }
    _, n2, err := net.ParseCIDR("172.16.0.0/16")
    if err != nil {
        panic(err)
    }
    _, n3, err := net.ParseCIDR("192.168.0.0/16")
    if err != nil {
        panic(err)
    }
    _, n4, err := net.ParseCIDR("127.0.0.0/8")
    if err != nil {
        panic(err)
    }

    PrivateNets = append(PrivateNets, n1, n2, n3, n4)

    // configure http handlers
    Http.RegisterHandler("/", OnPrivStaticSrvUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/", OnPubStaticSrvUri, PUBLIC_HANDLER)
    Http.RegisterHandler("/cmd/crash/", OnCrashUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/cmd/shutdown/", OnShutdownUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/counters/", OnCountersUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/netinfo/", OnNetInfoUri, PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/", http.HandlerFunc(pprof.Index), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/profile", http.HandlerFunc(pprof.Profile), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol), PRIVATE_HANDLER)
    Http.RegisterHandler("/debug/pprof/trace", http.HandlerFunc(pprof.Trace), PRIVATE_HANDLER)
}
