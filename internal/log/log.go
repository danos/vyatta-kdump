// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package log

import (
	"log"
	"log/syslog"
	"os"
)

var (
	Elog *log.Logger
	Dlog *log.Logger
	Ilog *log.Logger
	Wlog *log.Logger
)

func newLogger(p syslog.Priority, logFlag int) (*log.Logger, error) {
	s, err := syslog.New(p, "vci-kdump")
	if err != nil {
		return nil, err
	}
	return log.New(s, "", logFlag), nil
}

func init() {
	// Use syslog if it is available, otherwise fallback
	// to something sensible.
	var err error
	Dlog, err = newLogger(syslog.LOG_DEBUG, 0)
	if err != nil {
		Dlog = log.New(os.Stdout, "DEBUG: ", 0)
		Dlog.Println(err)
	}

	Elog, err = newLogger(syslog.LOG_ERR, 0)
	if err != nil {
		Elog = log.New(os.Stderr, "ERROR: ", 0)
		Elog.Println(err)
	}

	Ilog, err = newLogger(syslog.LOG_INFO, 0)
	if err != nil {
		Ilog = log.New(os.Stdout, "INFO: ", 0)
		Ilog.Println(err)
	}

	Wlog, err = newLogger(syslog.LOG_WARNING, 0)
	if err != nil {
		Wlog = log.New(os.Stderr, "WARNING: ", 0)
		Wlog.Println(err)
	}
}
