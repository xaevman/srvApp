//  ---------------------------------------------------------------------------
//
//  run.go
//
//  Copyright (c) 2015, Jared Chavez. 
//  All rights reserved.
//
//  Use of this source code is governed by a BSD-style
//  license that can be found in the LICENSE file.
//
//  -----------

// +build !windows

package srvApp

func run() {
    if !startSingleton() {
        shutdownChan<- true
    }

    blockUntilShutdown()
}
