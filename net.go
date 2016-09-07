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

	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HandlerType enumeration.
const (
	PRIVATE_HANDLER = iota
	PUBLIC_HANDLER
	ALL_HANDLER
)

// config syncronization
var (
	netCfgLock sync.RWMutex
)

// privateNets returns a list of private, reserved, network segments.
func PrivateNets() []*net.IPNet {
	return privateNets
}

var privateNets []*net.IPNet

// localAddrs returns a list of addresses local to the machine the
// application is running on.
func LocalAddrs() []*net.IP {
	return localAddrs
}

var localAddrs []*net.IP

type Conn struct {
	net.Conn
	buffer *bufio.Reader
}

func (bc *Conn) Read(b []byte) (int, error) {
	return bc.buffer.Read(b)
}

type srvAppListener struct {
	listener net.Listener
	closing  int32
	config   *tls.Config
}

func (this *srvAppListener) Addr() net.Addr {
	return this.listener.Addr()
}

func (this *srvAppListener) Accept() (net.Conn, error) {
	c, err := this.listener.Accept()
	if err != nil {
		srvLog.Error("%s", err)
		return nil, err
	}

	if this.config == nil {
		srvLog.Debug("HTTP Conn intiialized (%s)", c.RemoteAddr())
		return c, err
	} else {
		srvLog.Debug("TLS Conn intiialized (%s)", c.RemoteAddr())
		return tls.Server(c, this.config), nil
	}
}

func (this *srvAppListener) Close() error {
	atomic.StoreInt32(&this.closing, 1)
	err := this.listener.Close()
	if err != nil {
		srvLog.Error("%v", err)
	}

	return err
}

func (this *srvAppListener) isClosing() bool {
	val := atomic.LoadInt32(&this.closing)
	return val == 1
}

func (this *srvAppListener) String() string {
	return this.listener.Addr().String()
}

func (this *srvAppListener) listen(addr string, tlsCfg *tls.Config) error {
	this.config = tlsCfg

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	this.listener = ln

	return nil
}

type UriHandler struct {
	Handler        http.Handler
	Pattern        string
	RequiredAccess int
}

type HttpSrv struct {
	privatePort      int
	privateTLSPort   int
	privateHandlers  map[string]*UriHandler
	privateListeners map[string]*srvAppListener
	privateMux       *XMux
	privateSrv       *http.Server
	privateStaticDir string
	configLock       sync.RWMutex
	publicPort       int
	publicTLSPort    int
	publicHandlers   map[string]*UriHandler
	publicListeners  map[string]*srvAppListener
	publicMux        *XMux
	publicSrv        *http.Server
	publicStaticDir  string
	tlsCfg           *tls.Config
}

func (this *HttpSrv) Configure(
	privatePort int,
	privateTLSPort int,
	privateStaticDir string,
	privateStaticAccessLevel int,
	publicPort int,
	publicTLSPort int,
	publicStaticDir string,
	publicStaticAccessLevel int,
	tlsRedirect bool,
	certMap map[string]*tls.Certificate,
	forceRestart bool,
) {
	this.configLock.Lock()
	defer this.configLock.Unlock()

	privateChanged := false
	publicChanged := false

	if this.privatePort != privatePort {
		this.privatePort = privatePort
		privateChanged = true
	}

	if this.privateTLSPort != privateTLSPort {
		this.privateTLSPort = privateTLSPort
		privateChanged = true
	}

	if this.privateStaticDir != privateStaticDir {
		this.privateStaticDir = privateStaticDir
		if privateStaticDir != "" {
			httpSrv.registerHandler(
				"/",
				OnPrivStaticSrvUri,
				PRIVATE_HANDLER,
				privateStaticAccessLevel,
			)
		}
		privateChanged = true
	}

	if this.publicPort != publicPort {
		this.publicPort = publicPort
		publicChanged = true
	}

	if this.publicTLSPort != publicTLSPort {
		this.publicTLSPort = publicTLSPort
		publicChanged = true
	}

	if this.publicStaticDir != publicStaticDir {
		this.publicStaticDir = publicStaticDir
		if publicStaticDir != "" {
			httpSrv.registerHandler(
				"/",
				OnPubStaticSrvUri,
				PUBLIC_HANDLER,
				publicStaticAccessLevel,
			)
		}
		publicChanged = true
	}

	this.configureTLS(certMap, tlsRedirect)

	if privateChanged || forceRestart {
		this.restartPrivateHttp()
	}

	if publicChanged || forceRestart {
		this.restartPublicHttp()
	}
}

func (this *HttpSrv) IsPrivateNetwork(ip string) bool {
	for i := range privateNets {
		ip := net.ParseIP(ip)
		if privateNets[i].Contains(ip) {
			return true
		}
	}

	return false
}

func (this *HttpSrv) configureTLS(certMap map[string]*tls.Certificate, tlsRedirect bool) {
	if len(certMap) > 0 {
		certs := make([]tls.Certificate, 0, len(certMap))
		for k := range certMap {
			certs = append(certs, *certMap[k])
		}

		this.tlsCfg = &tls.Config{
			Certificates:      certs,
			NameToCertificate: certMap,
			NextProtos:        []string{"http/1.1"},
		}

		if this.privateTLSPort > 0 && tlsRedirect {
			this.privateMux.TLSRedirect = true
		} else {
			this.privateMux.TLSRedirect = false
		}

		if this.publicTLSPort > 0 && tlsRedirect {
			this.publicMux.TLSRedirect = true
		} else {
			this.publicMux.TLSRedirect = false
		}
	} else {
		this.tlsCfg = nil
		this.privateMux.TLSRedirect = false
		this.publicMux.TLSRedirect = false
	}
}

func (this *HttpSrv) getNetInfo() map[string]map[string][]string {
	this.configLock.RLock()
	defer this.configLock.RUnlock()

	privHandlers := make([]string, 0, len(this.privateHandlers))
	privServers := make([]string, 0, len(this.privateListeners))
	pubHandlers := make([]string, 0, len(this.publicHandlers))
	pubServers := make([]string, 0, len(this.publicListeners))

	for k := range this.privateHandlers {
		privHandlers = append(privHandlers, k)
	}
	sort.Strings(privHandlers)

	for k := range this.privateListeners {
		privServers = append(privServers, k)
	}
	sort.Strings(privServers)

	for k := range this.publicHandlers {
		pubHandlers = append(pubHandlers, k)
	}
	sort.Strings(pubHandlers)

	for k := range this.publicListeners {
		pubServers = append(pubServers, k)
	}
	sort.Strings(pubServers)

	handlers := make(map[string]map[string][]string)
	handlers["private"] = make(map[string][]string)
	handlers["public"] = make(map[string][]string)

	handlers["private"]["servers"] = privServers
	handlers["private"]["handlers"] = privHandlers
	handlers["public"]["servers"] = pubServers
	handlers["public"]["handlers"] = pubHandlers

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
	// grab private network addresses
	httpList := make(map[string]string)
	TLSList := make(map[string]string)
	for x := range localAddrs {
		if this.IsPrivateNetwork(localAddrs[x].String()) {
			if this.privatePort > 0 {
				httpAddr := net.JoinHostPort(
					localAddrs[x].String(),
					fmt.Sprintf("%d", this.privatePort),
				)
				httpList[httpAddr] = httpAddr
			}

			if this.privateTLSPort > 0 {
				tlsAddr := net.JoinHostPort(
					localAddrs[x].String(),
					fmt.Sprintf("%d", this.privateTLSPort),
				)
				TLSList[tlsAddr] = tlsAddr
			}
		}
	}

	// shut down old listeners
	for k := range this.privateListeners {
		_, ok := httpList[k]
		if ok {
			delete(httpList, k)
			continue
		}

		_, ok = TLSList[k]
		if ok {
			delete(TLSList, k)
			continue
		}

		// old listener - shut it down
		this.privateListeners[k].Close()
		delete(this.privateListeners, k)
	}

	// start up new http listeners
	for i := range httpList {
		ln := &srvAppListener{}
		err := ln.listen(httpList[i], nil)
		if err != nil {
			srvLog.Error("%v", err)
			continue
		}

		this.privateListeners[httpList[i]] = ln

		srvLog.Debug("Initializing PrivateHttp %s", httpList[i])
		go func(ln *srvAppListener) {
			defer crash.HandleAll()

			err := this.privateSrv.Serve(ln)
			if ln.isClosing() {
				srvLog.Info("Shutting down listener %s", ln)
				return
			}

			if err != nil {
				srvLog.Error("%v", err)
			}
		}(ln)
	}

	// start up TLS listeners
	for i := range TLSList {
		ln := &srvAppListener{}
		err := ln.listen(TLSList[i], this.tlsCfg)
		if err != nil {
			srvLog.Error("%v", err)
			continue
		}

		this.privateListeners[TLSList[i]] = ln

		srvLog.Debug("Initializing PrivateTLS %s", TLSList[i])
		go func(ln *srvAppListener) {
			defer crash.HandleAll()

			err := this.privateSrv.Serve(ln)
			if ln.isClosing() {
				srvLog.Info("Shutting down listener %s", ln)
				return
			}

			if err != nil {
				srvLog.Error("%v", err)
			}
		}(ln)
	}
}

func (this *HttpSrv) restartPublicHttp() {
	// grab private network addresses
	httpList := make(map[string]string)
	TLSList := make(map[string]string)
	for x := range localAddrs {
		local := false

		for y := range privateNets {
			ip := net.ParseIP(localAddrs[x].String())
			if privateNets[y].Contains(ip) {
				local = true
				break
			}
		}

		if !local {
			if this.publicPort > 0 {
				httpAddr := net.JoinHostPort(
					localAddrs[x].String(),
					fmt.Sprintf("%d", this.publicPort),
				)
				httpList[httpAddr] = httpAddr
			}

			if this.publicTLSPort > 0 {
				tlsAddr := net.JoinHostPort(
					localAddrs[x].String(),
					fmt.Sprintf("%d", this.publicTLSPort),
				)
				TLSList[tlsAddr] = tlsAddr
			}
		}
	}

	// shut down old listeners
	for k := range this.publicListeners {
		_, ok := httpList[k]
		if ok {
			delete(httpList, k)
			continue
		}

		_, ok = TLSList[k]
		if ok {
			delete(TLSList, k)
			continue
		}

		// old listener - shut it down
		this.publicListeners[k].Close()
		delete(this.publicListeners, k)
	}

	// start up new http listeners
	for i := range httpList {
		ln := &srvAppListener{}
		err := ln.listen(httpList[i], nil)
		if err != nil {
			srvLog.Error("%v", err)
			continue
		}

		this.publicListeners[httpList[i]] = ln

		srvLog.Debug("Initializing PublicHttp %s", httpList[i])
		go func(ln *srvAppListener) {
			defer crash.HandleAll()

			err := this.publicSrv.Serve(ln)
			if ln.isClosing() {
				srvLog.Info("Shutting down listener %s", ln)
				return
			}

			if err != nil {
				srvLog.Error("%v", err)
			}
		}(ln)
	}

	// start up new TLS listeners
	for i := range TLSList {
		ln := &srvAppListener{}
		err := ln.listen(TLSList[i], this.tlsCfg)
		if err != nil {
			srvLog.Error("%v", err)
			continue
		}

		this.publicListeners[TLSList[i]] = ln

		srvLog.Debug("Initializing PublicHttp %s", TLSList[i])
		go func(ln *srvAppListener) {
			defer crash.HandleAll()

			err := this.publicSrv.Serve(ln)
			if ln.isClosing() {
				srvLog.Info("Shutting down listener %s", ln)
				return
			}

			if err != nil {
				srvLog.Error("%v", err)
			}
		}(ln)
	}
}

func (this *HttpSrv) RegisterHandler(
	path string,
	f func(http.ResponseWriter, *http.Request),
	handlerType byte,
	accessLevel int,
) {
	this.configLock.Lock()
	defer this.configLock.Unlock()

	this.registerHandler(path, f, handlerType, accessLevel)
}

func (this *HttpSrv) registerHandler(
	path string,
	f func(http.ResponseWriter, *http.Request),
	handlerType byte,
	accessLevel int,
) {
	uriHandler := &UriHandler{
		Handler:        http.HandlerFunc(f),
		Pattern:        path,
		RequiredAccess: accessLevel,
	}

	switch handlerType {
	case PRIVATE_HANDLER:
		this.privateHandlers[path] = uriHandler
		this.privateMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Private HttpHandler %s registered (%s)",
			path,
			AccessLevelStr[accessLevel],
		)
	case PUBLIC_HANDLER:
		this.publicHandlers[path] = uriHandler
		this.publicMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Public HttpHandler %s registered (%s)",
			path,
			AccessLevelStr[accessLevel],
		)
	case ALL_HANDLER:
		this.privateHandlers[path] = uriHandler
		this.privateMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Private HttpHandler %s registered (%s)",
			path,
			AccessLevelStr[accessLevel],
		)
		this.publicHandlers[path] = uriHandler
		this.publicMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Public HttpHandler %s registered (%s)",
			path,
			AccessLevelStr[accessLevel],
		)
	default:
		srvLog.Error("Unknown handler type (%d)", handlerType)
	}
}

func ValidateRequestBody(
	resp http.ResponseWriter,
	req *http.Request,
	buffer *bytes.Buffer,
) error {
	if req.ContentLength < 1 {
		return fmt.Errorf("Zero length body")
	}

	bodyLen, err := buffer.ReadFrom(req.Body)
	if err != nil {
		return err
	}

	lenHdrStr, ok := req.Header["Content-Length"]
	if ok {
		contentLen, err := strconv.ParseInt(lenHdrStr[0], 10, 64)
		if err == nil {
			if contentLen != req.ContentLength {
				return fmt.Errorf(
					"Content-Length header does not match request ContentLength proprety (%d != %d)",
					contentLen,
					req.ContentLength,
				)
			}
		}
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
	newPrivateMux := NewXMux()
	newPublicMux := NewXMux()

	newSrv := &HttpSrv{
		privatePort:      DefaultPrivateHttpPort,
		privateHandlers:  make(map[string]*UriHandler),
		privateListeners: make(map[string]*srvAppListener),
		privateMux:       newPrivateMux,
		privateSrv: &http.Server{
			Handler:      newPrivateMux,
			ReadTimeout:  time.Duration(10 * time.Second),
			WriteTimeout: time.Duration(10 * time.Second),
		},
		publicPort:      DefaultPublicHttpPort,
		publicHandlers:  make(map[string]*UriHandler),
		publicListeners: make(map[string]*srvAppListener),
		publicMux:       newPublicMux,
		publicSrv: &http.Server{
			Handler:      newPublicMux,
			ReadTimeout:  time.Duration(10 * time.Second),
			WriteTimeout: time.Duration(10 * time.Second),
		},
	}

	return newSrv
}

func netInit() {
	// populate list of local addresses
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	localAddrs = make([]*net.IP, 0)
	for i := range addrs {
		addrParts := strings.Split(addrs[i].String(), "/")
		ip := net.ParseIP(addrParts[0])
		if ip == nil {
			continue
		}

		if ip.To4() == nil {
			continue
		}

		localAddrs = append(localAddrs, &ip)
	}

	srvLog.Info("LocalAddresses: %v", localAddrs)

	// populate list of private address networks
	privateNets = make([]*net.IPNet, 0)

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

	privateNets = append(privateNets, n1, n2, n3, n4)

	ipsInit()
	geoInit()

	// configure http handlers
	httpSrv.RegisterHandler(
		"/cmd/crash/",
		OnCrashUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/cmd/shutdown/",
		OnShutdownUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/appinfo/",
		OnAppInfoUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/counters/",
		OnCountersUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/logs/",
		OnLogsUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/ping/",
		OnPingUri,
		ALL_HANDLER,
		ACCESS_LEVEL_USER,
	)

	httpSrv.RegisterHandler(
		"/debug/pprof/",
		http.HandlerFunc(pprof.Index),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/pprof/cmdline",
		http.HandlerFunc(pprof.Cmdline),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN)

	httpSrv.RegisterHandler(
		"/debug/pprof/profile",
		http.HandlerFunc(pprof.Profile),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/pprof/symbol",
		http.HandlerFunc(pprof.Symbol),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"/debug/pprof/trace",
		http.HandlerFunc(pprof.Trace),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)
}

func netShutdown() {
	geoShutdown()
	ipsShutdown()
}
