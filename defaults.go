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
	DefaultHttpLogBuffers = 100

	DefaultPrivateHttpPort          = 0
	DefaultPrivateTLSPort           = 0
	DefaultPrivateStaticDir         = ""
	DefaultPrivateStaticAccessLevel = "admin"
	DefaultPublicHttpPort           = 0
	DefaultPublicTLSPort            = 0
	DefaultPublicStaticDir          = ""
	DefaultPublicStaticAccessLevel  = "admin"
	DefaultTLSRedirect              = true
	DefaultHonorXForwardedFor       = false

	DefaultSmtpFromAddr = ""
	DefaultSmtpSrvAddr  = "smtp"
	DefaultSmtpSrvPort  = 25

	DefaultVerboseCrashReports = false
)
