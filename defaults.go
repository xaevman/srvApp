//  ---------------------------------------------------------------------------
//
//  defaults.go
//
//  Copyright (c) 2015, Jared Chavez. 
//  All rights reserved.
//
//  Use of this source code is governed by a BSD-style
//  license that can be found in the LICENSE file.
//
//  -----------

package srvApp

const (
    DefaultPrivateHttpEnabled = true
    DefaultPrivateHttpPort = 8081
    DefaultPrivateStaticDir = ""
    DefaultPublicHttpEnabled = false
    DefaultPublicHttpPort = 8080
    DefaultPublicStaticDir = ""

    DefaultSmtpFromAddr = ""
    DefaultSmtpSrvAddr = "smtp"
    DefaultSmtpSrvPort = 25
    
    DefaultVerboseCrashReports = false
)
