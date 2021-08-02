// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package vci_kdump

import (
	"fmt"
	"github.com/danos/vyatta-kdump/internal/kdump"
	rpc "github.com/danos/vyatta-kdump/internal/rpc"
	"os"
)

type RPC struct {
	conf *Config
}

func RPCNew(conf *Config) *RPC {
	return &RPC{
		conf: conf,
	}
}

func (r *RPC) DeleteCrashDumps(in rpc.RPCInput) (struct{}, error) {
	_, crashdumps := kdump.GetCrashFiles()
	if len(in.Index) == 0 {
		for _, dump := range crashdumps {
			kdump.DelCrashDump(dump)
		}
		return struct{}{}, nil
	}

	bad_index := make([]int32, 0)
	dumps_to_delete := make([]os.FileInfo, 0)
	for _, index := range in.Index {
		n, err := dumpIndex(index, len(crashdumps))
		if err != nil {
			bad_index = append(bad_index, index)
		} else {
			dumps_to_delete = append(dumps_to_delete, crashdumps[n])
		}
	}
	if len(bad_index) != 0 {
		return struct{}{}, fmt.Errorf("DeleteCrashDumps bad input: %v", bad_index)
	}
	for _, d := range dumps_to_delete {
		kdump.DelCrashDump(d)
	}
	return struct{}{}, nil
}

func (r *RPC) GetCrashDmesg(in rpc.RPCInput) (*rpc.CrashDMesgOut, error) {
	crash_dir, crashdumps := kdump.GetCrashFiles()

	res := &rpc.CrashDMesgOut{}
	if len(in.Index) != 0 {
		res.CrashInfo = make([]rpc.CrashData, len(in.Index))
		for i, index := range in.Index {
			res.CrashInfo[i].Index = index
		}
	} else {
		res.CrashInfo = make([]rpc.CrashData, len(crashdumps))
		for i := 0; i < len(crashdumps); i++ {
			res.CrashInfo[i].Index = int32(i)
		}
	}

	for i := 0; i < len(res.CrashInfo); i++ {
		n, err := dumpIndex(res.CrashInfo[i].Index, len(crashdumps))
		if err != nil {
			continue
		}
		res.CrashInfo[i].FileName = fmt.Sprintf("%s/%s", crash_dir, crashdumps[n].Name())
		res.CrashInfo[i].DMesg = kdump.GetCrashDMsg(crashdumps[n])
	}
	return res, nil
}

func dumpIndex(n int32, ndumps int) (int, error) {
	if int(n) >= ndumps || int(-n) > ndumps {
		return -1, fmt.Errorf("Error: Index (%d) out of range [%d..%d]\n", n, -ndumps, ndumps-1)
	}
	return int(n) % ndumps, nil
}
