// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package kdump

import (
	"bytes"
	"errors"
	"fmt"
	systemd "github.com/coreos/go-systemd/dbus"
	"github.com/danos/vyatta-kdump/internal/log"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

const (
	kdumpLoadService                  = "vyatta-kdump-load.service"
	kdumpEnvFile                      = "/etc/default/kdump-tools"
	kdumpCmd                          = "/usr/sbin/kdump-config"
	kdumpCrashDir                     = "/var/crash"
	kdumpDir                          = "/var/lib/kdump"
	kdumpLastBootFile                 = "kdump-last-boot-crashed"
	kernelCmdLine                     = "/proc/cmdline"
	grubEditEnvCmd                    = "/opt/vyatta/sbin/vyatta-grub-editenv"
	initrdStateFile                   = kdumpDir + ".initrd-created"
	kexecCrashSizePath                = "/sys/kernel/kexec_crash_size"
	kexecCrashLoadedPath              = "/sys/kernel/kexec_crash_loaded"
	kdumpCrashKernelMemDefault        = "2432-8G:384M,8G-:512M"
	kdumpCrashKernelMemMin            = 256
	kdumpMinUnreserved                = 2048
	kdumpKernel                       = "/boot/vmlinuz"
	kdumpInitrd                string = "/boot/initrd.img"
	runDir                            = "/run"
)

const (
	KDumpNotReady = iota
	KDumpReady
)

const envFile = `### Autogenerate by vci-kdump
### Note: Manual change to this file will be lost during next commit
### kdump-tools defaults are in comments.
USE_KDUMP=1
#KDUMP_SYSCTL="kernel.panic_on_oops=1"
KDUMP_SYSCTL=""
KDUMP_KERNEL={{.Kernel}}
KDUMP_INITRD={{.Initrd}}
#KDUMP_FAIL_CMD="reboot -f"
#KDUMP_DUMP_DMESG=
KDUMP_COREDIR="/var/crash"
KDUMP_DUMP_DMESG=1
KDUMP_NUM_DUMPS={{.NumDumps}}
KDUMP_DELETE_OLD={{.DeleteOld}}
#MAKEDUMP_ARGS="-c -d 31"
#KDUMP_KEXEC_ARGS=""
#KDUMP_CMDLINE=""
KDUMP_CMDLINE_APPEND="nr_cpus=1 systemd.unit=vyatta-kdump-dump.service irqpoll nousb ata_piix.prefer_ms_hyperv=0"
`

var CrashKernelMemory uint  // from /sys/kernel/kexec_crash_size
var CrashKernelParam string // Kernel command line parameter "crashkernel"
var envFileTemplate *template.Template
var lastBootCrashStatus string

func init() {
	envFileTemplate = template.Must(template.New("KDumpEnv").Parse(envFile))
	var err error
	CrashKernelMemory, err = GetCrashKernelMemory()
	if err != nil {
		log.Wlog.Println("CrashKernelMemory Error:", err)
	}
	CrashKernelParam, err = GetCrashKernelParam()
	if err != nil {
		log.Wlog.Println("Error in getting CrashKernelMemory:", err)
	}
	lastBootCrashStatus = getLastBootCrashStatus()
}

// return crash kernel memory in bytes
func GetCrashKernelMemory() (uint, error) {
	memstr, err := ioutil.ReadFile(kexecCrashSizePath)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(memstr))
	mem, err := strconv.ParseUint(s, 10, 0)
	return uint(mem), err
}

// Get current kernel's crashkernel cmdline parameter value
func GetCrashKernelParam() (string, error) {
	cmdline, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`crashkernel=[^ ]+`)
	ck := re.Find(cmdline)
	if ck == nil {
		return "", nil
	}
	return strings.TrimPrefix(strings.TrimSpace(string(ck)), "crashkernel="), nil
}

func GetKDumpState() int {
	out, err := ioutil.ReadFile(kexecCrashLoadedPath)
	if err != nil {
		log.Elog.Println("Cannot Read File %s: %v", kexecCrashLoadedPath, err)
		return KDumpNotReady
	}
	s := strings.TrimSpace(string(out))
	status, err := strconv.ParseInt(s, 10, 0)
	if err != nil {
		log.Elog.Printf("Invalid state %s from %s: %s", string(out), kexecCrashLoadedPath, err)
		return KDumpNotReady
	}
	if status == 1 {
		return KDumpReady
	}
	return KDumpNotReady
}

func WriteEnv(numfile *int, delete_old bool) error {
	envInput := struct {
		NumDumps  string
		DeleteOld string
		Kernel    string
		Initrd    string
	}{"", "0", kdumpKernel, kdumpInitrd}
	if numfile != (*int)(nil) {
		envInput.NumDumps = strconv.FormatInt(int64(*numfile), 10)
	}
	if delete_old {
		envInput.DeleteOld = "1"
	}
	var envbuf bytes.Buffer
	err := envFileTemplate.Execute(&envbuf, &envInput)
	if err != nil {
		return fmt.Errorf("WriteEnv template error: %s", err)
	}
	result := envbuf.Bytes()

	old, _ := ioutil.ReadFile(kdumpEnvFile)
	// Nothing changed - don't write to file
	if bytes.Compare(old, result) == 0 {
		return nil
	}
	return safeWriteFile(kdumpEnvFile, result)
}

// Setup all files needed to enable Kdump and starts
// Kdump. This doesn't update kernel cmdline in grub.
func Enable(numdumps *int, delete_old bool) error {
	err := WriteEnv(numdumps, delete_old)
	if err != nil {
		return err
	}

	// do not return error if the crashkernel cmdline parameter is missing
	if CrashKernelParam == "" {
		return nil
	}

	if CrashKernelMemory == 0 {
		return errors.New("No Crash Kernel Memory reserved. Not starting KDump")
	}

	// If Kdump is already loaded no need to restart.
	if GetKDumpState() == KDumpReady {
		log.Ilog.Printf("No need to restart Kernel Crash Dump Service")
		return nil
	}
	if err = startSystemdService(kdumpLoadService); err != nil {
		log.Elog.Printf("Failed to start kdumpLoadService:%s", err.Error())
		return err
	}
	return nil
}

// Disable and Cleanup files if requested.
func Disable(cleanup bool) {
	if err := stopSystemdService(kdumpLoadService); err != nil {
		log.Dlog.Printf("Failed to stop %s:%s", kdumpLoadService, err)
	}
	if cleanup {
		os.Remove(kdumpEnvFile)
	}
}

// Start a systemd service
func startSystemdService(srv string) error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	ch := make(chan string)
	if _, err := conn.StartUnit(srv, "replace", ch); err != nil {
		return err
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("Failed to start Unit %s: result=%s", srv, result)
	}
	return nil
}

// Stop a systemd service
func stopSystemdService(srv string) error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	ch := make(chan string)
	_, err = conn.StopUnit(srv, "replace", ch)
	if err != nil {
		return err
	}
	result := <-ch
	if result == "failed" || result == "timeout" {
		return fmt.Errorf("failed to stop %s: result=%s", srv, result)
	}
	return nil
}

// make crashkernel parameters value from config
func crashKernelMemFromCfg(cfgmem string) (string, error) {
	if cfgmem == "auto" {
		return kdumpCrashKernelMemDefault, nil
	}

	mem, err := strconv.ParseInt(cfgmem, 10, 32)
	if err == nil && mem >= kdumpCrashKernelMemMin {
		return fmt.Sprintf("%dM-:%dM", kdumpMinUnreserved+mem, mem), nil
	}
	if err == nil {
		err = errors.New(fmt.Sprintf("%sM too small, need at least %dM", cfgmem, kdumpCrashKernelMemMin))
	}
	return "", err
}

// Get Currently set crashkernel Memory in Grub and then update that.
func ReserveMem(cfgmem string) error {
	if cfgmem == "0" {
		out, err := exec.Command(grubEditEnvCmd, "--running", "--action=unset", "crashkernel_mem").Output()
		if err != nil {
			log.Dlog.Printf("Free reserved memory: out=%s, err=%s", out, err)
		}
		return err
	}
	grubenvval, err := crashKernelMemFromCfg(cfgmem)
	if err == nil {
		_, err = exec.Command(grubEditEnvCmd, "--running", "--action=set", "crashkernel_mem="+grubenvval).Output()
	}
	return err
}

// Get crashkernel_mem from grubenv
func GrubReservedMem() string {
	out, err := exec.Command(grubEditEnvCmd, "--running", "--action=list").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimPrefix(line, "crashkernel_mem=")
		if line != trimmed {
			return trimmed
		}
	}
	return ""
}

// Check if /proc/cmdline matches with the grubenv
func IsRebootNeeded() bool {
	if GrubReservedMem() == CrashKernelParam {
		return false
	}
	return true
}

func isCrashDir(dentry os.FileInfo) bool {
	if !dentry.IsDir() {
		return false
	}
	name := dentry.Name()
	if len(name) != 12 { // YYYYYMMDDhhmm
		return false
	}
	year, err := strconv.ParseUint(name[:4], 10, 0)
	if err != nil || year < 1970 { // Start of epoch
		return false
	}
	month, err := strconv.ParseUint(name[4:6], 10, 0)
	if err != nil || month > 12 {
		return false
	}
	day, err := strconv.ParseUint(name[4:6], 10, 0)
	if err != nil || day > 31 {
		return false
	}
	_, err = GetCrashSize(name)
	if err != nil {
		return false
	}
	dumpfile := fmt.Sprintf("%s/%s/dump.%s", kdumpCrashDir, name, name)
	out, err := exec.Command("/usr/bin/file", "--brief", dumpfile).Output()
	if err != nil {
		return false
	}
	if bytes.HasPrefix(out, []byte("Kdump compressed dump")) {
		return true
	}
	return false
}

func GetCrashSize(name string) (int64, error) {
	dumpfile := fmt.Sprintf("%s/%s/dump.%s", kdumpCrashDir, name, name)
	dump_fi, err := os.Stat(dumpfile)
	if err != nil {
		return 0, err
	}
	if !dump_fi.Mode().IsRegular() {
		return 0, errors.New(dumpfile + ":Not a regular file")
	}
	if dump_fi.Size() == 0 {
		return 0, errors.New(dumpfile + ":Zero sized file")
	}
	return dump_fi.Size(), nil
}

func GetCrashFiles() (string, []os.FileInfo) {
	crashfiles := make([]os.FileInfo, 0)
	dentries, err := ioutil.ReadDir(kdumpCrashDir)
	if err != nil {
		return kdumpCrashDir, crashfiles
	}
	for _, dentry := range dentries {
		if isCrashDir(dentry) {
			crashfiles = append(crashfiles, dentry)
		}
	}
	sort.SliceStable(crashfiles, func(i, j int) bool {
		ni, _ := strconv.ParseUint(crashfiles[i].Name(), 10, 0)
		nj, _ := strconv.ParseUint(crashfiles[j].Name(), 10, 0)
		return nj < ni
	})
	return kdumpCrashDir, crashfiles
}

// Get Kdump dmesg file from Crash Dump Name
func GetCrashDMsg(crashdump os.FileInfo) string {
	dname := crashdump.Name()
	fname := fmt.Sprintf("%s/%s/dmesg.%s", kdumpCrashDir, dname, dname)
	dmesg, _ := ioutil.ReadFile(fname)
	return string(dmesg)
}

func DelCrashDump(crashdump os.FileInfo) error {
	dname := fmt.Sprintf("%s/%s", kdumpCrashDir, crashdump.Name())
	if err := os.RemoveAll(dname); err != nil {
		log.Ilog.Printf("DelCrashdump: %s\n", err)
		return err
	}
	return nil
}

func LastBootCrashed() bool {
	return lastBootCrashStatus != ""
}

func getLastBootCrashStatus() (string) {
	read_status := func(dname string) string {
		fname := fmt.Sprintf("%s/%s", dname, kdumpLastBootFile)
		if st, err := ioutil.ReadFile(fname); err == nil && len(st) != 0 {
			if err == nil {
				return string(st)
			}
		}
		return ""
	}
	dirs := []string{kdumpCrashDir, runDir}
	for _, d := range dirs {
		st := read_status(d)
		if st == "" {
			continue
		}
		var ts, bootid, status string
		n, err := fmt.Sscanf(st, "timestamp=%s bootid=%s status=%s",
			&ts, &bootid, &status)
		if err != nil || n != 3 {
			continue
		}
		logLastBootCrashStatus(status, ts)
		return status
	}
	return ""
}

func logLastBootCrashStatus(status string, ts string) {
	msg := "System rebooted due to a system crash."
	switch status {
	case "":
		// do nothing - no kernel crash
	case "success":
		log.Elog.Printf("%s Kernel crash dump file is at %s/%s/.", msg, kdumpCrashDir, ts)
	case "skipped":
		log.Elog.Printf("%s Kernel crash dump not saved, 'files-to-save' limit reached.", msg)
	case "nofile":
		log.Elog.Printf("%s Failed to create Kernel Crash dump file.", msg)
	case "error":
		log.Elog.Printf("%s Error while capturing kernel crash dump.", msg)
	default:
		log.Elog.Printf("%s Kernel crash dump status is \"%s\".", status)
	}
}

func safeWriteFile(name string, data []byte) error {
	dir, fname := path.Split(name)
	tmpf, err := ioutil.TempFile(dir, fname)
	if err != nil {
		return fmt.Errorf("WriteEnv: %s", err)
	}
	tmpname := tmpf.Name()
	defer os.Remove(tmpname)

	if _, err = tmpf.Write(data); err != nil {
		return err
	}
	tmpf.Sync()

	if err := tmpf.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpname, kdumpEnvFile); err != nil {
		return err
	}
	return nil
}
