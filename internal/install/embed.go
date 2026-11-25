package install

import _ "embed"

//go:embed data/vpnctld.service
var unitFile []byte

//go:embed data/org.vpnctl.daemon.policy
var polkitPolicy []byte

//go:embed data/vpnctl-autostart.desktop
var autostartDesktop []byte

const (
	UnitName       = "vpnctld.service"
	UnitPath       = "/lib/systemd/system/vpnctld.service"
	PolkitPath     = "/usr/share/polkit-1/actions/org.vpnctl.daemon.policy"
	AutostartName  = "vpnctl.desktop"
	SystemLibDir   = "/usr/lib/vpnctl"
	SystemBinVpnctl = "/usr/bin/vpnctl"
	SystemBinGUI   = "/usr/bin/vpnctl-gui"
)
