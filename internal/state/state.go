// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package state

type KDumpStatusData struct {
	ServiceState      string          `rfc7951:"service-state,omitempty"`
	ReservedMemory    uint64          `rfc7951:"reserved-memory"`
	NeedReboot        bool            `rfc7951:"need-reboot"`
	CrashRebootStatus bool            `rfc7951:"rebooted-after-system-crash,omitempty"`
	CrashDumps        []CrashDumpData `rfc7951:"crash-dump-files"`
}

type CrashDumpData struct {
	Index     uint32 `rfc7951:"index"`
	Timestamp string `rfc7951:"timestamp,omitempty"`
	Path      string `rfc7951:"path,omitempty"`
	Size      uint64 `rfc7951:"size,omitempty"`
}
