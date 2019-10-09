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

type SubnetInfo struct {
	Servers  []string
	Handlers []string
}

type NetInfo struct {
	Private *SubnetInfo
	Public  *SubnetInfo
}

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

// synchronization around netid
var (
	netId string
)

// privateNets returns a list of private, reserved, network segments.
func PrivateNets() []*net.IPNet {
	return privateNets
}

var privateNets []*net.IPNet

// localAddrs returns a list of addresses local to the machine the
// application is running on.
func LocalAddrs() []*net.IP {
	localAddrsLock.RLock()
	defer localAddrsLock.RUnlock()

	return localAddrs
}

var (
	localAddrsLock sync.RWMutex
	localAddrs     []*net.IP
)

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
		if !this.isClosing() {
			srvLog.Error("%s", err)
		}
		return nil, err
	}

	if this.config == nil {
		//srvLog.Debug("HTTP Conn intiialized (%s)", c.RemoteAddr())
		return c, err
	} else {
		//srvLog.Debug("TLS Conn intiialized (%s)", c.RemoteAddr())
		return tls.Server(c, this.config), nil
	}
}

func (this *srvAppListener) Close() error {
	if this.isClosing() {
		return nil
	}

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
	Method         string
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
	ignoreAddrs      []string
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
	ignoreAddrs []string,
	tlsRedirect bool,
	honorXForwardedFor bool,
	certMap map[string]*tls.Certificate,
	forceRestart bool,
) {
	this.configLock.Lock()
	defer this.configLock.Unlock()

	this.ignoreAddrs = ignoreAddrs

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
				"GET",
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
				"GET",
				"/",
				OnPubStaticSrvUri,
				PUBLIC_HANDLER,
				publicStaticAccessLevel,
			)
		}
		publicChanged = true
	}

	this.configureTLS(certMap, tlsRedirect)

	this.publicMux.HonorXForwardedFor = honorXForwardedFor
	this.privateMux.HonorXForwardedFor = honorXForwardedFor

	if privateChanged || forceRestart {
		this.restartPrivateHttp()
	}

	if publicChanged || forceRestart {
		this.restartPublicHttp()
	}
}

func (this *HttpSrv) isIgnoreAddr(ip string) bool {
	for i := range this.ignoreAddrs {
		if this.ignoreAddrs[i] == ip {
			return true
		}
	}

	return false
}

func (this *HttpSrv) IsPrivateNetwork(ip string) bool {
	if this.isIgnoreAddr(ip) {
		return false
	}

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

func (this *HttpSrv) GetSrvAddrs() []string {
	result := make([]string, 0)

	this.configLock.RLock()
	defer this.configLock.RUnlock()

	for k := range this.privateListeners {
		if len(k) > 0 {
			result = append(result, k)
		}
	}

	for k := range this.publicListeners {
		if len(k) > 0 {
			result = append(result, k)
		}
	}

	return result
}

func (this *HttpSrv) getNetInfo() *NetInfo {
	this.configLock.RLock()
	defer this.configLock.RUnlock()

	net := &NetInfo{
		Private: &SubnetInfo{
			Servers:  make([]string, 0, len(this.privateListeners)),
			Handlers: make([]string, 0, len(this.privateHandlers)),
		},
		Public: &SubnetInfo{
			Servers:  make([]string, 0, len(this.publicListeners)),
			Handlers: make([]string, 0, len(this.publicHandlers)),
		},
	}

	for k := range this.privateHandlers {
		net.Private.Handlers = append(net.Private.Handlers, k)
	}
	sort.Strings(net.Private.Handlers)

	for k := range this.privateListeners {
		net.Private.Servers = append(net.Private.Servers, k)
	}
	sort.Strings(net.Private.Servers)

	for k := range this.publicHandlers {
		net.Public.Handlers = append(net.Public.Handlers, k)
	}
	sort.Strings(net.Public.Handlers)

	for k := range this.publicListeners {
		net.Public.Servers = append(net.Public.Servers, k)
	}
	sort.Strings(net.Public.Servers)

	return net
}

func (this *HttpSrv) Shutdown() {
	this.configLock.Lock()
	defer this.configLock.Unlock()

	srvLog.Info("net Shutdown")
	for _, ln := range this.privateListeners {
		ln.Close()
	}

	for _, ln := range this.publicListeners {
		ln.Close()
	}
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

	localAddrsLock.RLock()
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
	localAddrsLock.RUnlock()

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

	localAddrsLock.RLock()
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
	localAddrsLock.RUnlock()

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

func (this *HttpSrv) RemoveHandler(method string, path string) {
	this.configLock.Lock()
	defer this.configLock.Unlock()

	this.removeHandler(method, path)
}

func (this *HttpSrv) removeHandler(method string, path string) {
	delete(this.privateHandlers, makeKey(method, path))
	this.privateMux.RemoveHandleFunc(method, path)

	delete(this.publicHandlers, makeKey(method, path))
	this.publicMux.RemoveHandleFunc(method, path)

	srvLog.Info(
		"HttpHandler %s unregistered",
		path,
	)
}

func (this *HttpSrv) RegisterHandler(
	method string,
	path string,
	f func(http.ResponseWriter, *http.Request),
	handlerType byte,
	accessLevel int,
) {
	this.configLock.Lock()
	defer this.configLock.Unlock()

	this.registerHandler(method, path, f, handlerType, accessLevel)
}

func (this *HttpSrv) registerHandler(
	method string,
	path string,
	f func(http.ResponseWriter, *http.Request),
	handlerType byte,
	accessLevel int,
) {
	// verify the path is valid
	if len(path) == 0 || path[0] != '/' {
		panic(fmt.Sprintf("route path must begin with a '/'. got='%s'", path))
	}

	uriHandler := &UriHandler{
		Method:         method,
		Handler:        http.HandlerFunc(f),
		Pattern:        path,
		RequiredAccess: accessLevel,
	}

	key := makeKey(method, path)

	switch handlerType {
	case PRIVATE_HANDLER:
		this.privateHandlers[key] = uriHandler
		this.privateMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Private HttpHandler %s %s registered (%s)",
			method,
			path,
			AccessLevelStr[accessLevel],
		)
	case PUBLIC_HANDLER:
		this.publicHandlers[key] = uriHandler
		this.publicMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Public HttpHandler %s %s registered (%s)",
			method,
			path,
			AccessLevelStr[accessLevel],
		)
	case ALL_HANDLER:
		this.privateHandlers[key] = uriHandler
		this.privateMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Private HttpHandler %s %s registered (%s)",
			method,
			path,
			AccessLevelStr[accessLevel],
		)
		this.publicHandlers[key] = uriHandler
		this.publicMux.HandleFunc(uriHandler)
		srvLog.Info(
			"Public HttpHandler %s %s registered (%s)",
			method,
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
	newPrivateMux := NewXMux()
	newPublicMux := NewXMux()

	newSrv := &HttpSrv{
		privatePort:      DefaultPrivateHttpPort,
		privateHandlers:  make(map[string]*UriHandler),
		privateListeners: make(map[string]*srvAppListener),
		privateMux:       newPrivateMux,
		privateSrv: &http.Server{
			Handler:      newPrivateMux,
			ReadTimeout:  time.Duration(60 * time.Minute),
			WriteTimeout: time.Duration(60 * time.Minute),
		},
		publicPort:      DefaultPublicHttpPort,
		publicHandlers:  make(map[string]*UriHandler),
		publicListeners: make(map[string]*srvAppListener),
		publicMux:       newPublicMux,
		publicSrv: &http.Server{
			Handler:      newPublicMux,
			ReadTimeout:  time.Duration(60 * time.Minute),
			WriteTimeout: time.Duration(60 * time.Minute),
		},
	}

	return newSrv
}

func netGetNetId() string {
	netCfgLock.Lock()
	defer netCfgLock.Unlock()
	return netId
}

func netInit() {
	// populate list of local addresses
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	localAddrsLock.Lock()
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
	localAddrsLock.Unlock()

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
		"POST",
		"/cmd/crash/",
		OnCrashUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"POST",
		"/cmd/shutdown/",
		OnShutdownUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"POST",
		"/cmd/update_config/",
		OnUpdateConfigUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"POST",
		"/cmd/rotate_logs/",
		OnRotateLogsUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/appinfo/",
		OnAppInfoUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/config/",
		OnConfigUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/counters/",
		OnCountersUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/logs/",
		OnLogsUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/ping/",
		OnPingUri,
		ALL_HANDLER,
		ACCESS_LEVEL_NONE,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/pprof/",
		http.HandlerFunc(pprof.Index),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/pprof/cmdline",
		http.HandlerFunc(pprof.Cmdline),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/pprof/profile",
		http.HandlerFunc(pprof.Profile),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/pprof/symbol",
		http.HandlerFunc(pprof.Symbol),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	httpSrv.RegisterHandler(
		"GET",
		"/debug/pprof/trace",
		http.HandlerFunc(pprof.Trace),
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)
}

func netShutdown() {
	httpSrv.Shutdown()
	geoShutdown()
	ipsShutdown()
}
