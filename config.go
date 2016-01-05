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
    "github.com/xaevman/flog"
    "github.com/xaevman/ini"
)

func onCfgChange(cfg *ini.IniCfg, changeCount int) {
    Log.Info("Config change detected (%s) sequence: %d", cfg.Name, changeCount)
    cfgApp(cfg)
    cfgCrashReports(cfg)
    cfgNet(cfg)
}

func cfgApp(cfg *ini.IniCfg) {
    sec := cfg.GetSection("app")

    val  := sec.GetFirstVal("DebugLogs")
    bVal := val.GetValBool(0, false)
    Log.Info("Debug logs enabled: %t", bVal)
    Log.SetDebugLogsEnabled(bVal)

    val = sec.GetFirstVal("DebugFlushIntervalSec")
    iVal := int32(val.GetValInt(0, flog.DefaultFlushIntervalSec))
    Log.SetDebugFlushIntervalSec(iVal)
    Log.Debug("DebugFlushIntervalSec set (%d)", iVal)

    val = sec.GetFirstVal("InfoFlushIntervalSec")
    iVal = int32(val.GetValInt(0, flog.DefaultFlushIntervalSec))
    Log.SetInfoFlushIntervalSec(iVal)
    Log.Debug("InfoFlushIntervalSec set (%d)", iVal)
}

func cfgCrashReports(cfg *ini.IniCfg) {
    sec := cfg.GetSection("crash_reports")

    val  := sec.GetFirstVal("VerboseCrashReports")
    bVal := val.GetValBool(0, DefaultVerboseCrashReports)
    Log.Debug("VerboseCrashReports set (%t)", bVal)
    crash.SetVerboseCrashReport(bVal)

    val = sec.GetFirstVal("SmtpSrvAddr")
    sVal := val.GetValStr(0, DefaultSmtpSrvAddr)
    Log.Debug("SmtpSrvAddr set (%s)", sVal)
    EmailCrashHandler.SrvAddr = sVal

    val = sec.GetFirstVal("SmtpSrvPort")
    iVal := val.GetValInt(0, DefaultSmtpSrvPort)
    Log.Debug("SmtpSrvPort set (%d)", iVal)
    EmailCrashHandler.SrvPort = iVal

    val = sec.GetFirstVal("SmtpUser")
    EmailCrashHandler.SrvUser = val.GetValStr(0, "")

    val = sec.GetFirstVal("SmtpPass")
    EmailCrashHandler.SrvPass = val.GetValStr(0, "")

    val = sec.GetFirstVal("SmtpFromAddr")
    sVal = val.GetValStr(0, DefaultSmtpFromAddr)
    Log.Debug("SmtpFromAddr set (%s)", sVal)
    EmailCrashHandler.FromAddr = sVal

    vals := sec.GetVals("SmtpToAddr")
    EmailCrashHandler.ClearToAddrs()
    for i := range vals {
        sVal = vals[i].GetValStr(0, "")
        if sVal != "" {
            Log.Debug("Added ToAddr: %s", sVal)
            EmailCrashHandler.ToAddrs = append(
                EmailCrashHandler.ToAddrs, 
                sVal,
            )
        }
    }
}

func cfgNet(cfg *ini.IniCfg) {
    sec := cfg.GetSection("net")

    val            := sec.GetFirstVal("PrivateHttpEnabled")
    privateEnabled := val.GetValBool(0, DefaultPrivateHttpEnabled)
    Log.Debug("PrivateHttpEnabled: %t", privateEnabled)

    val        = sec.GetFirstVal("PrivateHttpPort")
    privatePort := val.GetValInt(0, DefaultPrivateHttpPort)
    Log.Debug("PrivateHttpPort: %d", privatePort)

    val  = sec.GetFirstVal("PublicHttpEnabled")
    publicEnabled := val.GetValBool(0, DefaultPublicHttpEnabled)
    Log.Debug("PublicHttpEnabled: %t", publicEnabled)

    val        = sec.GetFirstVal("PublicHttpPort")
    publicPort := val.GetValInt(0, DefaultPublicHttpPort)
    Log.Debug("PublicHttpPort: %d", publicPort)

    Http.Configure(privateEnabled, privatePort, publicEnabled, publicPort)
}
