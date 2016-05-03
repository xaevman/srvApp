package srvApp

import (
    "fmt"
    "net"
    "net/http"
    "net/url"
    "path"
    "strings"
    "sync"
    "sync/atomic"

    "github.com/xaevman/counters"
)

var muxId uint64

type XMux struct {
    id    uint64
    mu    sync.RWMutex
    m     map[string]muxEntry
    hosts bool // whether any patterns contain hostnames

    // counters
    cntParseFailed  counters.Counter
    cntNotFound     counters.Counter
    cntGeoDeny      counters.Counter
    cntIPDeny       counters.Counter
    cntThrottleDeny counters.Counter
    cntSuccess      counters.Counter
}

type muxEntry struct {
    explicit bool
    h        *UriHandler
    pattern  string
}

// NewServeMux allocates and returns a new XMux.
func NewXMux() *XMux { 
    newId  := atomic.AddUint64(&muxId, 1)
    newMux := &XMux{
        id              : newId,
        m               : make(map[string]muxEntry),
        cntParseFailed  : counters.NewUint(fmt.Sprintf("net.xmux.%d.parse_failed", newId)),
        cntNotFound     : counters.NewUint(fmt.Sprintf("net.xmux.%d.handler_not_found", newId)),
        cntGeoDeny      : counters.NewUint(fmt.Sprintf("net.xmux.%d.geo_deny", newId)),
        cntIPDeny       : counters.NewUint(fmt.Sprintf("net.xmux.%d.ip_deny", newId)),
        cntThrottleDeny : counters.NewUint(fmt.Sprintf("net.xmux.%d.throttle_deny", newId)),
        cntSuccess      : counters.NewUint(fmt.Sprintf("net.xmux.%d.success", newId)),
    }

    appCounters.Add(newMux.cntParseFailed)
    appCounters.Add(newMux.cntNotFound)
    appCounters.Add(newMux.cntGeoDeny)
    appCounters.Add(newMux.cntIPDeny)
    appCounters.Add(newMux.cntThrottleDeny)
    appCounters.Add(newMux.cntSuccess)

    return newMux
}

// Does path match pattern?
func pathMatch(pattern, path string) bool {
    if len(pattern) == 0 {
        // should not happen
        return false
    }
    n := len(pattern)
    if pattern[n-1] != '/' {
        return pattern == path
    }
    return len(path) >= n && path[0:n] == pattern
}

// Return the canonical path for p, eliminating . and .. elements.
func cleanPath(p string) string {
    if p == "" {
        return "/"
    }
    if p[0] != '/' {
        p = "/" + p
    }
    np := path.Clean(p)
    // path.Clean removes trailing slash except for root;
    // put the trailing slash back if necessary.
    if p[len(p)-1] == '/' && np != "/" {
        np += "/"
    }
    return np
}

// Find a handler on a handler map given a path string
// Most-specific (longest) pattern wins
func (mux *XMux) match(path string) (h *UriHandler, pattern string) {
    var n = 0
    for k, v := range mux.m {
        if !pathMatch(k, path) {
            continue
        }
        if h == nil || len(k) > n {
            n = len(k)
            h = v.h
            pattern = v.pattern
        }
    }

    return
}

// Handler returns the handler to use for the given request,
// consulting r.Method, r.Host, and r.URL.Path. It always returns
// a non-nil handler. If the path is not in its canonical form, the
// handler will be an internally-generated handler that redirects
// to the canonical path.
//
// Handler also returns the registered pattern that matches the
// request or, in the case of internally-generated redirects,
// the pattern that will match after following the redirect.
//
// If there is no registered handler that applies to the request,
// Handler returns a ``page not found'' handler and an empty pattern.
func (mux *XMux) Handler(r *http.Request) (h *UriHandler, pattern string) {
    if r.Method != "CONNECT" {
        if p := cleanPath(r.URL.Path); p != r.URL.Path {
            _, pattern = mux.handler(r.Host, p)
            url := *r.URL
            url.Path = p
            redirect := &UriHandler {
                Handler        : http.RedirectHandler(url.String(), http.StatusMovedPermanently),
                Pattern        : pattern,
                RequiredAccess : ACCESS_LEVEL_NONE,
            }
            return redirect, pattern
        }
    }

    return mux.handler(r.Host, r.URL.Path)
}

// handler is the main implementation of Handler.
// The path is known to be in canonical form, except for CONNECT methods.
func (mux *XMux) handler(host, path string) (h *UriHandler, pattern string) {
    mux.mu.RLock()
    defer mux.mu.RUnlock()

    // Host-specific pattern takes precedence over generic ones
    if mux.hosts {
        h, pattern = mux.match(host + path)
    }
    if h == nil {
        h, pattern = mux.match(path)
    }
    return
}

// ServeHTTP dispatches the request to the handler whose
// pattern most closely matches the request URL.
func (mux *XMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.RequestURI == "*" {
        if r.ProtoAtLeast(1, 1) {
            w.Header().Set("Connection", "close")
        }
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    h, pattern := mux.Handler(r)

    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        srvLog.Error("Unable to parse remote host address (%s)", r.RemoteAddr)
        http.Error(w, "StatusInternalServerError", http.StatusInternalServerError)
        mux.cntParseFailed.Add(uint64(1))
        return
    }

    // handler not found
    if h == nil {
        http.NotFoundHandler().ServeHTTP(w, r)
        ipsLogStats(host, r.URL.Path, pattern, ACCESS_LEVEL_NONE, REQ_STATUS_NOT_FOUND)
        mux.cntNotFound.Add(uint64(1))
        return
    }

    // handler ip authorization
    accessLevel := GetAccessLevel(host)
    if accessLevel < h.RequiredAccess {
        srvLog.Error(
            "Access level insufficient %s -> %s (%d < %d)", 
            host, 
            r.URL.Path,
            accessLevel, 
            h.RequiredAccess,
        )
        http.Error(w, "StatusUnauthorized", http.StatusUnauthorized)
        ipsLogStats(host, r.URL.Path, pattern, accessLevel, REQ_STATUS_IP_DENY)
        mux.cntIPDeny.Add(uint64(1))
        return
    }

    // geoIP authorization
    country, ok := geoAuthorizeHost(host) 
    if !ok {
        srvLog.Error(
            "Host blocked by geo-ip lookup %s -> %s (country %s)", 
            host, 
            r.URL.Path,
            country, 
        )
        http.Error(w, "StatusUnauthorized", http.StatusUnauthorized)
        ipsLogStats(host, r.URL.Path, pattern, accessLevel, REQ_STATUS_GEO_DENY)
        mux.cntGeoDeny.Add(uint64(1))
        return
    }

    // everything is good. handle the request
    h.Handler.ServeHTTP(w, r)
    ipsLogStats(host, r.URL.Path, pattern, accessLevel, REQ_STATUS_SUCCESS)
    mux.cntSuccess.Add(uint64(1))
}

// Handle registers the handler for the given pattern.
// If a handler already exists for pattern, Handle panics.
//func (mux *XMux) Handle(pattern string, handler http.Handler) {
func (mux *XMux) Handle(uriHandler *UriHandler) {
    mux.mu.Lock()
    defer mux.mu.Unlock()

    if uriHandler.Pattern == "" {
        panic("http: invalid pattern " + uriHandler.Pattern)
    }
    if uriHandler.Handler == nil {
        panic("http: nil handler")
    }
    if mux.m[uriHandler.Pattern].explicit {
        panic("http: multiple registrations for " + uriHandler.Pattern)
    }

    mux.m[uriHandler.Pattern] = muxEntry{
        explicit : true,
        h        : uriHandler,
        pattern  : uriHandler.Pattern,
    }

    if uriHandler.Pattern[0] != '/' {
        mux.hosts = true
    }

    // Helpful behavior:
    // If pattern is /tree/, insert an implicit permanent redirect for /tree.
    // It can be overridden by an explicit registration.
    n := len(uriHandler.Pattern)
    if n > 0 && uriHandler.Pattern[n-1] == '/' && !mux.m[uriHandler.Pattern[0:n-1]].explicit {
        // If pattern contains a host name, strip it and use remaining
        // path for redirect.
        path := uriHandler.Pattern
        if uriHandler.Pattern[0] != '/' {
            // In pattern, at least the last character is a '/', so
            // strings.Index can't be -1.
            path = uriHandler.Pattern[strings.Index(uriHandler.Pattern, "/"):]
        }
        url := &url.URL{Path: path}
        helper := &UriHandler {
            Handler        : http.RedirectHandler(url.String(), http.StatusMovedPermanently),
            Pattern        : uriHandler.Pattern[0:n-1],
            RequiredAccess : uriHandler.RequiredAccess,
        }

        mux.m[uriHandler.Pattern[0:n-1]] = muxEntry{
            h       : helper, 
            pattern : uriHandler.Pattern,
        }
    }
}

// HandleFunc registers the handler function for the given pattern.
func (mux *XMux) HandleFunc(uriHandler *UriHandler) {
    mux.Handle(uriHandler)
}
