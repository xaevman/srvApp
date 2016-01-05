package srvApp

import (
    "github.com/xaevman/crash"

    "fmt"
    "net"
    "net/http"
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

func (this *srvAppListener) Close() {
    atomic.StoreInt32(&this.closing, 1)
    err := this.listener.Close()
    if err != nil {
        Log.Error("%v", err)
    }
}

func (this *srvAppListener) Closing() bool {
    val := atomic.LoadInt32(&this.closing)
    return val == 1
}

func (this *srvAppListener) Listen(addr string) error {
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
    configLock       sync.RWMutex
    publicEnabled    bool
    publicPort       int
    publicHandlers   map[string]func(http.ResponseWriter, *http.Request)
    publicListeners  map[string]*srvAppListener
    publicMux        *http.ServeMux
    publicSrv        *http.Server
}

func (this *HttpSrv) Configure(
    privateEnabled  bool,
    privatePort     int,
    publicEnabled bool,
    publicPort    int,
) {
    privateChanged  := false
    publicChanged := false

    this.configLock.Lock()
    defer this.configLock.Unlock()

    if this.privateEnabled != privateEnabled {
        this.privateEnabled = privateEnabled
        privateChanged = true
    }

    if this.privatePort != privatePort {
        this.privatePort = privatePort
        privateChanged = true
    }
    
    if this.publicEnabled != publicEnabled {
        this.publicEnabled = publicEnabled
        publicChanged = true
    }

    if this.publicPort != publicPort {
        this.publicPort = publicPort
        publicChanged = true
    }

    if privateChanged {
        this.restartPrivateHttp()
    }

    if publicChanged {
        this.restartPublicHttp()
    }
}

func (this *HttpSrv) GetHandlerKeys() map[string][]string {
    this.configLock.RLock()
    defer this.configLock.RUnlock()

    priv := make([]string, 0, len(this.privateHandlers))
    pub  := make([]string, 0, len(this.publicHandlers))

    for k, _ := range this.privateHandlers {
        priv = append(priv, k)
    }
    sort.Strings(priv)

    for k, _ := range this.publicHandlers {
        pub = append(pub, k)
    }
    sort.Strings(pub)

    handlers           := make(map[string][]string)
    handlers["private"] = priv
    handlers["public"]  = pub

    return handlers
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
        this.privateListeners[k].Close()
        delete(this.privateListeners, k)
    }

    // start up new listeners
    if !this.privateEnabled {
        return
    }

    for i := range addrList {
        ln  := &srvAppListener{}
        err := ln.Listen(addrList[i])
        if err != nil {
            Log.Error("%v", err)
            continue
        }

        this.privateListeners[addrList[i]] = ln
                
        Log.Debug("Initializing PrivateHttp %s", addrList[i])
        go func(ln *srvAppListener) {
            defer crash.HandleAll()

            err := this.privateSrv.Serve(ln.listener)
            if ln.Closing() {
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
        this.publicListeners[k].Close()
        delete(this.publicListeners, k)
    }

    // start up new listeners
    if !this.publicEnabled {
        return
    }

    for i := range addrList {
        ln  := &srvAppListener{}
        err := ln.Listen(addrList[i])
        if err != nil {
            Log.Error("%v", err)
            continue
        }

        this.publicListeners[addrList[i]] = ln
                
        Log.Debug("Initializing PublicHttp %s", addrList[i])
        go func(ln *srvAppListener) {
            defer crash.HandleAll()

            err := this.publicSrv.Serve(ln.listener)
            if ln.Closing() {
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
        break
    case PUBLIC_HANDLER:
        this.publicHandlers[path] = f
        this.publicMux.HandleFunc(path, f)
        Log.Info("Public HttpHandler %s registered", path)
        break
    case ALL_HANDLER:
        this.privateHandlers[path] = f
        this.privateMux.HandleFunc(path, f)
        Log.Info("Private HttpHandler %s registered", path)
        this.publicHandlers[path] = f
        this.publicMux.HandleFunc(path, f)
        Log.Info("Public HttpHandler %s registered", path)
        break
    default:
        Log.Error("Unknown handler type (%d)", handlerType)
        break
    }
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
}
