// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"flag"
	"fmt"
	configd "github.com/danos/configd/client"
	configd_rpc "github.com/danos/configd/rpc"
	"github.com/danos/encoding/rfc7951"
	"github.com/danos/utils/pathutil"
	cf "github.com/danos/vyatta-kdump/internal/config"
	rpc "github.com/danos/vyatta-kdump/internal/rpc"
	st "github.com/danos/vyatta-kdump/internal/state"
	"os"
	"strconv"
	"strings"
	"text/template"
)

const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB
)

const statusTemplate = `
{{- $hdr_fmt := "%6.6s  %24.24s  %20.20s  %16.16s"}}
{{- $fmt := "%6d  %24.24s  %20.20s  %16d"}}
Kernel Crash Dump Status : {{.OpStatus}}{{- if .Status.NeedReboot }} (Next Boot: {{.CfgState}}), Reboot Needed{{end}}
  Reserved Memory : {{.ReservedMemoryFromStatus}} (Configured: {{.ReservedMemStr}})
  Number of Captured Kernel Crash Dumps: {{.CrashCount}}
{{if .CrashCount}}
{{- printf $hdr_fmt "Index" "Path" "Timestamp" "Size"}}
{{ repeat "_" 72}}
{{range .Status.CrashDumps -}}
{{printf $fmt .Index .Path .Timestamp .Size}}
{{end}}
{{end}}
`

func main() {
	arg_show := flag.Bool("show", false, "Show Kernel crash dumps")
	arg_msg := flag.Bool("message", false, "Show Crash dump messages")
	arg_del := flag.Bool("delete", false, "Delete Kernel Crash Dumps")
	arg_allowed := flag.Bool("allowed", false, "Delete Kernel Crash Dumps")

	flag.Parse()
	if flag.NFlag() != 1 {
		flag.PrintDefaults()
		os.Exit(1)
	}

	req_list := make([]int, 0)
	for _, arg := range flag.Args() {
		n, err := strconv.ParseInt(arg, 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Ignoring Inavlid Kernel Crash Dump Index %s", arg)
		} else {
			req_list = append(req_list, int(n))
		}
	}
	var err error
	if *arg_show {
		err = showKDump()
	} else if *arg_msg {
		err = showDMsg(req_list)
	} else if *arg_del {
		err = delKDump(req_list)
	} else if *arg_allowed {
		err = allowed()
	}
	errExit(err)
}

func errExit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func showKDump() error {
	kd, err := getKDumpFullTree()
	const cmd = "Show kernel crash dumps"

	if err != nil {
		return fmt.Errorf("%s:%s", cmd, err)
	}
	if kd == nil || kd.Status == nil {
		return fmt.Errorf("%s:Status unavailable.", cmd)
	}
	t := template.New("Status")
	t.Funcs(template.FuncMap{"repeat": strings.Repeat})
	tmpl := template.Must(t.Parse(statusTemplate))
	if err := tmpl.Execute(os.Stdout, kd); err != nil {
		return fmt.Errorf("%s:Output template failed:%s", cmd, err)
	}
	return nil
}

func showDMsg(index []int) error {
	const cmd = "Show Kernel crash dump message"
	res := &rpc.CrashDMesgOut{}
	if err := callKDumpRPC("get-crash-dmesg", index, res); err != nil {
		return fmt.Errorf("%s:%s", cmd, err)
	}
	for _, ci := range res.CrashInfo {
		if ci.FileName != "" {
			fmt.Printf("Kernel dmesg for Crash Dump %d:%s\n", ci.Index, ci.FileName)
			fmt.Println(ci.DMesg)
			fmt.Printf("\n\n")
		} else {
			fmt.Fprintf(os.Stderr, "%s:Ignoring Invalid index %d\n", cmd, ci.Index)
		}
	}
	return nil
}

func delKDump(index []int) error {
	var res struct{}
	if err := callKDumpRPC("delete-crash-dumps", index, &res); err != nil {
		return fmt.Errorf("delete kernel-crash-dump error:%s", err)
	}
	return nil
}

func allowed() error {
	kd, err := getKDumpFullTree()
	if err != nil {
		return err
	}
	n := kd.CrashCount()
	switch {
	case n == 0:
		break
	case n < 3:
		for i := -n; i < n; i++ {
			fmt.Printf("%d\n", i)
		}
	default:
		fmt.Printf("0..%d\n", n)
	}
	return nil
}

func callKDumpRPC(name string, dump_index []int, data interface{}) error {
	client, err := configd.Connect()
	if err != nil {
		return err
	}
	defer client.Close()

	in := &rpc.RPCInput{make([]int32, len(dump_index))}
	for i, index := range dump_index {
		in.Index[i] = int32(index)
	}

	js_input, err := rfc7951.Marshal(in)
	if err != nil {
		return err
	}

	js, err := client.CallRpc("vyatta-system-crash-dump-v1", name, string(js_input), "rfc7951")
	if err == nil {
		err = rfc7951.Unmarshal([]byte(js), data)
	}
	return err
}

type KDumpFull struct {
	cf.KDumpData
	Status *st.KDumpStatusData `rfc7951:"status"`
}

func (kd *KDumpFull) OpStatus() string {
	if kd.Status == nil || kd.Status.ServiceState == "" {
		return "unknown"
	}
	return kd.Status.ServiceState
}

func (kd *KDumpFull) CfgState() string {
	if kd.IsEnabled() {
		return "enabled"
	}
	return "disabled"
}

func (kd *KDumpFull) ReservedMemoryFromStatus() string {
	m := kd.Status.ReservedMemory
	if m == 0 {
		return "0 bytes"
	}
	u := "bytes"
	if m%GB == 0 {
		m = m / GB
		u = "GB"
	} else if m%MB == 0 {
		m = m / MB
		u = "MB"
	} else if m%KB == 0 {
		m = m / KB
		u = "KB"
	}
	return fmt.Sprintf("%d %s", m, u)
}

func (kd *KDumpFull) CrashCount() int {
	return len(kd.Status.CrashDumps)
}

func getKDumpFullTree() (*KDumpFull, error) {
	client, err := configd.Connect()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	js, err := client.TreeGetFull(
		configd_rpc.AUTO,
		pathutil.Pathstr([]string{"system", "kernel-crash-dump"}),
		"rfc7951",
	)
	if err != nil {
		return nil, err
	}
	var kdump struct {
		KDump *KDumpFull `rfc7951:"vyatta-system-crash-dump-v1:kernel-crash-dump"`
	}
	err = rfc7951.Unmarshal([]byte(js), &kdump)
	if err != nil {
		return nil, err
	}
	return kdump.KDump, nil
}
