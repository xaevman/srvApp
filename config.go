//  ---------------------------------------------------------------------------
//
//  config.go
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
	"crypto/tls"
	"net"
	"path/filepath"

	"github.com/xaevman/crash"
	"github.com/xaevman/ini"
	"github.com/xaevman/log/flog"
)

// onCfgChange is called then the main application config changes. It then
// passes the config information on to other functions to handle configuration
// of different parts of the application.
func onCfgChange(cfg *ini.IniCfg, changeCount int) {
	srvLog.Info("Config change detected (%s) sequence: %d", cfg.Name, changeCount)
	cfgCrashReports(cfg)
	cfgLogs(cfg)
	cfgNet(cfg, changeCount)
}

// cfgLogs configures logging options when the application config changes.
func cfgLogs(cfg *ini.IniCfg) {
	sec := cfg.GetSection("app")

	val := sec.GetFirstVal("DebugLogs")
	bVal := val.GetValBool(0, false)
	srvLog.Info("Debug logs enabled: %t", bVal)
	srvLog.SetLogsEnabled("debug", bVal)

	val = sec.GetFirstVal("DebugFlushIntervalSec")
	i32Val := int32(val.GetValInt(0, flog.DefaultFlushIntervalSec))
	srvLog.SetFlushIntervalSec("debug", i32Val)
	srvLog.Info("DebugFlushIntervalSec set (%d)", i32Val)

	val = sec.GetFirstVal("InfoFlushIntervalSec")
	i32Val = int32(val.GetValInt(0, flog.DefaultFlushIntervalSec))
	srvLog.SetFlushIntervalSec("info", i32Val)
	srvLog.Info("InfoFlushIntervalSec set (%d)", i32Val)

}

// cfgCrashReports configures crash reporting options when the application
// config changes.
func cfgCrashReports(cfg *ini.IniCfg) {
	sec := cfg.GetSection("crash_reports")

	val := sec.GetFirstVal("VerboseCrashReports")
	bVal := val.GetValBool(0, DefaultVerboseCrashReports)
	srvLog.Info("VerboseCrashReports set (%t)", bVal)
	crash.SetVerboseCrashReport(bVal)

	val = sec.GetFirstVal("SmtpSrvAddr")
	sVal := val.GetValStr(0, DefaultSmtpSrvAddr)
	srvLog.Info("SmtpSrvAddr set (%s)", sVal)
	emailCrashHandler.SrvAddr = sVal

	val = sec.GetFirstVal("SmtpSrvPort")
	iVal := val.GetValInt(0, DefaultSmtpSrvPort)
	srvLog.Info("SmtpSrvPort set (%d)", iVal)
	emailCrashHandler.SrvPort = iVal

	val = sec.GetFirstVal("SmtpUser")
	emailCrashHandler.SrvUser = val.GetValStr(0, "")

	val = sec.GetFirstVal("SmtpPass")
	emailCrashHandler.SrvPass = val.GetValStr(0, "")

	val = sec.GetFirstVal("SmtpFromAddr")
	sVal = val.GetValStr(0, DefaultSmtpFromAddr)
	srvLog.Info("SmtpFromAddr set (%s)", sVal)
	emailCrashHandler.FromAddr = sVal

	vals := sec.GetVals("SmtpToAddr")
	emailCrashHandler.ClearToAddrs()
	for i := range vals {
		sVal = vals[i].GetValStr(0, "")
		if sVal != "" {
			srvLog.Info("Added ToAddr: %s", sVal)
			emailCrashHandler.ToAddrs = append(
				emailCrashHandler.ToAddrs,
				sVal,
			)
		}
	}
}

// cfgNet configures network options when the application config changes.
func cfgNet(cfg *ini.IniCfg, changeCount int) {
	netCfgLock.Lock()
	defer netCfgLock.Unlock()

	if !runCfg.InitSrvCmd && runMode != RUN_SVC {
		return
	}

	if !runCfg.InitSrvSvc && runMode == RUN_SVC {
		return
	}

	sec := cfg.GetSection("net")

	val := sec.GetFirstVal("PrivateHttpPort")
	privatePort := val.GetValInt(0, DefaultPrivateHttpPort)
	srvLog.Info("PrivateHttpPort: %d", privatePort)

	val = sec.GetFirstVal("PrivateStaticDir")
	privateStaticDir := val.GetValStr(0, DefaultPrivateStaticDir)
	privateStaticAccessLevel := parseAccessLevel(val.GetValStr(1, DefaultPrivateStaticAccessLevel))
	srvLog.Info("PrivateStaticDir: %s", privateStaticDir)

	val = sec.GetFirstVal("PublicHttpPort")
	publicPort := val.GetValInt(0, DefaultPublicHttpPort)
	srvLog.Info("PublicHttpPort: %d", publicPort)

	val = sec.GetFirstVal("PublicStaticDir")
	publicStaticDir := val.GetValStr(0, DefaultPublicStaticDir)
	publicStaticAccessLevel := parseAccessLevel(val.GetValStr(1, DefaultPublicStaticAccessLevel))
	srvLog.Info("PUblicStaticDir: %s", publicStaticDir)

	// TLS
	val = sec.GetFirstVal("TLSRedirect")
	tlsRedirect := val.GetValBool(0, DefaultTLSRedirect)
	srvLog.Info("TLSRedirect: %t", tlsRedirect)

	// XForwardedFor
	val = sec.GetFirstVal("HonorXForwardedFor")
	honorXForwardedFor := val.GetValBool(0, DefaultHonorXForwardedFor)
	srvLog.Info("HonorXForwardedFor: %t", honorXForwardedFor)

	val = sec.GetFirstVal("PrivateTLSPort")
	privateTLSPort := val.GetValInt(0, DefaultPrivateTLSPort)
	srvLog.Info("PrivateTLSPort: %d", privateTLSPort)

	val = sec.GetFirstVal("PublicTLSPort")
	publicTLSPort := val.GetValInt(0, DefaultPublicTLSPort)
	srvLog.Info("PrivateTLSPort: %d", publicTLSPort)

	certMap := make(map[string]*tls.Certificate)
	vals := sec.GetVals("CertMap")
	for i := range vals {
		domain := vals[i].GetValStr(0, "")
		cert := vals[i].GetValStr(1, "")
		key := vals[i].GetValStr(2, "")

		if len(domain) < 1 || len(cert) < 1 || len(key) < 1 {
			continue
		}

		certPath := filepath.Join(configDir, cert)
		keyPath := filepath.Join(configDir, key)

		kp, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			srvLog.Error("%s", err)
			continue
		}

		certMap[domain] = &kp

		srvLog.Info("CertMap defined: %s -> Cert: %s, Key: %s", domain, cert, key)
	}

	// Access control
	netAccessList = make([]*AccessNet, 0)
	vals = sec.GetVals("AccessRights")
	for i := range vals {
		ip, ipNet, err := net.ParseCIDR(vals[i].GetValStr(0, ""))
		if err != nil {
			srvLog.Info("%v", err)
			continue
		}

		level := parseAccessLevel(vals[i].GetValStr(1, "none"))
		newNet := true
		for i := range netAccessList {
			if netAccessList[i].Subnet.Contains(ip) {
				newNet = false
				if level < netAccessList[i].Level {
					netAccessList[i].Level = level
					srvLog.Info(
						"Updating net %s access level: %d",
						ip.String(),
						level,
					)
				}
			}
		}

		if newNet {
			netAccessList = append(netAccessList, &AccessNet{
				Level:  level,
				Subnet: ipNet,
			})
			srvLog.Info(
				"Registered network %s with access level %d",
				ip.String(),
				level,
			)
		}
	}

	// ignore addresses for listen binding
	ignoreAddrs := make([]string, 0)
	vals = sec.GetVals("BindIgnore")
	for i := range vals {
		addr := vals[i].GetValStr(0, "")
		if len(addr) > 0 {
			ignoreAddrs = append(ignoreAddrs, addr)
		}
	}

	// force "restart" on first config parse. This ensures that
	// that the http listeners are intialized if a user starts the
	// server with all default vaules.
	forceRestart := (changeCount == 0)

	httpSrv.Configure(
		privatePort,
		privateTLSPort,
		privateStaticDir,
		privateStaticAccessLevel,
		publicPort,
		publicTLSPort,
		publicStaticDir,
		publicStaticAccessLevel,
		ignoreAddrs,
		tlsRedirect,
		honorXForwardedFor,
		certMap,
		forceRestart,
	)

	netId = cfg.ConfigVer
	srvLog.Info("NetId: %s", netId)
}
