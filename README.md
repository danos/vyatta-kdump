This repository contains the yang modules, VCI service and packaging for vyatta-kdump feature.

*vci-kdump*
 - vci-kdump implement the VCI configuration, state, and rpc services for vyatta-system-crash-dump-v1 yang module.
vci-kdump creates the required configuration files for *kdump-tools* service and
launches the systemd kdump-tools service. On normal boot kdump-tool.service uses kexce to load the crash dump kernel.
When booted in the kdump kernel after a crash, it uses *makedumpfile* to capture the vmcore and save it to a file in
/var/crash directory.
The backend code reside in  internal/kdump/kdump.go.

