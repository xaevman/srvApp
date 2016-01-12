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
    "github.com/xaevman/crash"
    "github.com/xaevman/log/flog"
    "github.com/xaevman/ini"
)

func onCfgChange(cfg *ini.IniCfg, changeCount int) {
    srvLog.Info("Config change detected (%s) sequence: %d", cfg.Name, changeCount)
    cfgApp(cfg)
    cfgCrashReports(cfg)
    cfgNet(cfg, changeCount)
}

func cfgApp(cfg *ini.IniCfg) {
    sec := cfg.GetSection("app")

    val  := sec.GetFirstVal("DebugLogs")
    bVal := val.GetValBool(0, false)
    srvLog.Info("Debug logs enabled: %t", bVal)
    srvLog.SetLogsEnabled("debug", bVal)

    val = sec.GetFirstVal("DebugFlushIntervalSec")
    iVal := int32(val.GetValInt(0, flog.DefaultFlushIntervalSec))
    srvLog.SetFlushIntervalSec("debug", iVal)
    srvLog.Debug("DebugFlushIntervalSec set (%d)", iVal)

    val = sec.GetFirstVal("InfoFlushIntervalSec")
    iVal = int32(val.GetValInt(0, flog.DefaultFlushIntervalSec))
    srvLog.SetFlushIntervalSec("info", iVal)
    srvLog.Debug("InfoFlushIntervalSec set (%d)", iVal)
}

func cfgCrashReports(cfg *ini.IniCfg) {
    sec := cfg.GetSection("crash_reports")

    val  := sec.GetFirstVal("VerboseCrashReports")
    bVal := val.GetValBool(0, DefaultVerboseCrashReports)
    srvLog.Debug("VerboseCrashReports set (%t)", bVal)
    crash.SetVerboseCrashReport(bVal)

    val = sec.GetFirstVal("SmtpSrvAddr")
    sVal := val.GetValStr(0, DefaultSmtpSrvAddr)
    srvLog.Debug("SmtpSrvAddr set (%s)", sVal)
    emailCrashHandler.SrvAddr = sVal

    val = sec.GetFirstVal("SmtpSrvPort")
    iVal := val.GetValInt(0, DefaultSmtpSrvPort)
    srvLog.Debug("SmtpSrvPort set (%d)", iVal)
    emailCrashHandler.SrvPort = iVal

    val = sec.GetFirstVal("SmtpUser")
    emailCrashHandler.SrvUser = val.GetValStr(0, "")

    val = sec.GetFirstVal("SmtpPass")
    emailCrashHandler.SrvPass = val.GetValStr(0, "")

    val = sec.GetFirstVal("SmtpFromAddr")
    sVal = val.GetValStr(0, DefaultSmtpFromAddr)
    srvLog.Debug("SmtpFromAddr set (%s)", sVal)
    emailCrashHandler.FromAddr = sVal

    vals := sec.GetVals("SmtpToAddr")
    emailCrashHandler.ClearToAddrs()
    for i := range vals {
        sVal = vals[i].GetValStr(0, "")
        if sVal != "" {
            srvLog.Debug("Added ToAddr: %s", sVal)
            emailCrashHandler.ToAddrs = append(
                emailCrashHandler.ToAddrs, 
                sVal,
            )
        }
    }
}

func cfgNet(cfg *ini.IniCfg, changeCount int) {
    sec := cfg.GetSection("net")

    val            := sec.GetFirstVal("PrivateHttpEnabled")
    privateEnabled := val.GetValBool(0, DefaultPrivateHttpEnabled)
    srvLog.Debug("PrivateHttpEnabled: %t", privateEnabled)

    val        = sec.GetFirstVal("PrivateHttpPort")
    privatePort := val.GetValInt(0, DefaultPrivateHttpPort)
    srvLog.Debug("PrivateHttpPort: %d", privatePort)

    val               = sec.GetFirstVal("PrivateStaticDir")
    privateStaticDir := val.GetValStr(0, DefaultPrivateStaticDir)
    srvLog.Debug("PrivateStaticDir: %s", privateStaticDir)

    val  = sec.GetFirstVal("PublicHttpEnabled")
    publicEnabled := val.GetValBool(0, DefaultPublicHttpEnabled)
    srvLog.Debug("PublicHttpEnabled: %t", publicEnabled)

    val        = sec.GetFirstVal("PublicHttpPort")
    publicPort := val.GetValInt(0, DefaultPublicHttpPort)
    srvLog.Debug("PublicHttpPort: %d", publicPort)

    val              = sec.GetFirstVal("PublicStaticDir")
    publicStaticDir := val.GetValStr(0, DefaultPublicStaticDir)
    srvLog.Debug("PUblicStaticDir: %s", publicStaticDir)

    // force "restart" on first config parse. This ensures that
    // that the http listeners are intialized if a user starts the
    // server with all default vaules.
    forceRestart := (changeCount == 0)

    httpSrv.Configure(
        privateEnabled, 
        privatePort, 
        privateStaticDir,
        publicEnabled, 
        publicPort, 
        publicStaticDir,
        forceRestart,
    )
}
