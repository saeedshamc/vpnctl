<p align="center"><img src="assets/png/banner.png" alt="vpnctl" width="100%"></p>

# vpnctl

> Use the VPN running on your **Android phone** on your **Linux PC**, system-wide, over a USB cable.
> One self-contained package. No runtime dependencies. Tray icon, CLI and a local web UI.

**Languages:** English · [فارسی](README.fa.md)

**Status:** Phase 0 (design + assets). The code is delivered phase by phase, see [Phases](#12-phases).

---

## Table of contents

1. [What it does](#1-what-it-does)
2. [Goals and non-goals](#2-goals-and-non-goals)
3. [Requirements and how the design answers them](#3-requirements-and-how-the-design-answers-them)
4. [Architecture](#4-architecture)
5. [Connection logic, watchdog and recovery](#5-connection-logic-watchdog-and-recovery)
6. [Configuration](#6-configuration)
7. [CLI reference](#7-cli-reference)
8. [Tray and user interface](#8-tray-and-user-interface)
9. [Installer and packaging](#9-installer-and-packaging)
10. [Security model](#10-security-model)
11. [Lessons from field testing](#11-lessons-from-field-testing)
12. [Phases](#12-phases)
13. [Repository layout](#13-repository-layout)
14. [Risks and open decisions](#14-risks-and-open-decisions)
15. [Licensing](#15-licensing)

---

## 1. What it does

```
 Linux PC                                         Android phone
┌──────────────────────────────────┐   USB    ┌──────────────────────────────┐
│ apps → tun0 → tun2socks → 127.0.0.1:L ──────┼─► adb forward ─► 127.0.0.1:P │
│        (default route, DNS, IPv6)│  cable   │   v2rayNG (VPN app, SOCKS)   │
└──────────────────────────────────┘          │            │                 │
                                              │            ▼                 │
                                              │       VPN server → Internet  │
                                              └──────────────────────────────┘
```

* The phone runs a VPN client that exposes a local SOCKS5 proxy (v2rayNG: `127.0.0.1:10808`).
* `vpnctl` forwards that port over USB with `adb`, creates a virtual interface `tun0`, and uses `tun2socks` to push **all** traffic of the PC through it.
* The PC needs no VPN account, no config and no v2ray. The phone does the real connection.
* Default ports are `10808` (SOCKS) on both sides. Both are configurable, but only when you want a different one.

## 2. Goals and non-goals

**Goals**

* One command / one click to connect and disconnect; no leftover state afterwards.
* Safe by default: if anything fails, the normal internet connection must be restored automatically.
* Installable on a machine with **no internet** (everything needed is inside the package).
* Native look for a Linux desktop: application-menu entry, searchable, tray icon, polkit password prompt.
* The same behaviour from CLI, tray and UI (they share one engine).

**Non-goals**

* Not a VPN client itself (it does not speak vmess/vless/trojan). The phone app does.
* Not an adb/driver manager for every phone model.
* No iOS support in v1.
* No Windows/macOS support in v1.

## 3. Requirements and how the design answers them

| # | Requirement | Design answer |
|---|---|---|
| R1 | Name: **vpnctl**; CLI **and** GUI | Four binaries sharing one engine (see §4). |
| R2 | GUI without graphical frameworks (no Qt/GTK/Electron/Wails/Tauri) | The UI is plain HTML + CSS + vanilla JS **embedded in the Go binary** and served by Go's standard `net/http` on `127.0.0.1`. The tray icon speaks the D-Bus *StatusNotifierItem* protocol directly in Go. No toolkit is linked. |
| R3 | Behaves like PdaNet on Windows: background app, tray icon, right-click menu with **Connect**, click opens the window | `vpnctl-gui --tray-only` runs silently; the right-click menu has Connect / Disconnect / Open / Settings / Quit; left-click or *Open* shows the UI window. Closing the window only hides it. |
| R4 | Port is user-configurable; default 10808 | `phone_port` and `local_port` in config, `--phone-port/--local-port` in CLI, fields in Settings. |
| R5 | Installer with a graphical page; **also for .deb**, which is the main target | A `.deb` cannot draw its own window, so the graphical wizard is the **first-run wizard** of `vpnctl-gui` (launched automatically after install). A standalone `vpnctl-installer` (one self-contained file) shows the same wizard for non-deb systems. See §9. |
| R6 | No dependencies; works fully offline | Static Go binaries (`CGO_ENABLED=0`). `tun2socks` and `adb` are **bundled inside** the package. The `.deb` declares an empty `Depends:` line. The only things assumed are those every desktop Linux already has (kernel `tun`, systemd, polkit, a web browser for the UI). Details in §9.4. |
| R7 | Ask for the **root password** when connecting, like v2rayN on Linux | The privileged helper `vpnctld` is started through `pkexec` (polkit): the system's standard password dialog appears. The PC never stores the password and no passwordless `sudoers` rule is created. |
| R8 | Optionally start with the system; one of the **first options during install**, and togglable later in Settings | Wizard step 2 asks for it (default: on). Settings has the same switches. They enable/disable a systemd unit and an XDG autostart entry. |
| R9 | Icon in the application list, openable by search | `vpnctl.desktop` + icons installed to `hicolor` (16–512 px + SVG). Keywords: vpn, proxy, usb, tether, adb, v2ray. |
| R10 | A cool logo, stored in the repo assets folder | `assets/` (the folder is spelled `assets`): `logo.svg`, `icon.svg`, `banner.svg`, PNG sizes, 4 tray state icons. |

## 4. Architecture

Four small binaries, one shared engine:

```
vpnctl-gui  (user)  ──┐   tray + local web UI server
vpnctl      (user)  ──┼──►  unix socket /run/vpnctl/vpnctl.sock  ──►  vpnctld (root)
vpnctl-installer ─────┘                                              ├─ adb forward
                                                                      ├─ tun0 + tun2socks
                                                                      ├─ routes / DNS / IPv6
                                                                      ├─ watchdog
                                                                      └─ state file + cleanup
```

| Binary | Runs as | Purpose |
|---|---|---|
| `vpnctld` | **root** | The only privileged component. Does adb, tun, routes, DNS, IPv6, watchdog. Listens on a unix socket. |
| `vpnctl` | user | CLI. Sends commands to `vpnctld`; starts it through `pkexec` when it is not running. |
| `vpnctl-gui` | user | Tray icon + a tiny HTTP server on `127.0.0.1` that serves the embedded web UI. Opens the window in the user's browser in *app mode* (no tabs/address bar) when possible. |
| `vpnctl-installer` | user (asks for root) | Self-contained installer with the graphical wizard, for systems without the `.deb` flow. |

Only `vpnctld` has root. A bug in the UI or CLI cannot touch the network configuration directly.

### Internal packages (Go)

```
internal/config     load/save/validate config.toml
internal/adb        run bundled adb, device state, forward, authorise
internal/tunnel     create tun, supervise bundled tun2socks
internal/netcfg     routes, DNS (systemd-resolved), IPv6 sysctl, snapshot + restore
internal/watchdog   health checks and automatic rollback
internal/state      /run/vpnctl/state.json, crash recovery
internal/ipc        unix-socket JSON protocol + peer-credential check
internal/tray       StatusNotifierItem over D-Bus (pure Go)
internal/webui      http server, embedded assets, session token
internal/install    install/uninstall logic shared by wizard and .deb scripts
```

## 5. Connection logic, watchdog and recovery

### 5.1 `up` (idempotent)

1. If already connected: do nothing and report status. If half-connected: clean up first.
2. Check the phone: `adb` state must be `device` (not `unauthorized`/`offline`).
3. `adb forward tcp:LOCAL tcp:PHONE`.
4. **Probe the proxy before touching anything**: fetch `test_url` through `socks5h://127.0.0.1:LOCAL`. If it fails, stop with a clear message; the PC's network stays untouched.
5. Snapshot the current network state (default routes, DNS, IPv6 setting) to `state.json`.
6. Create `tun0`, assign `198.18.0.1/15`, start the bundled `tun2socks`, wait until it is really running.
7. Only now: default route `dev tun0 metric 1`, DNS on `tun0` (DNS-over-TLS), disable IPv6 if configured.
8. Report the exit IP and country.

### 5.2 `down`

Remove the route, stop `tun2socks`, revert DNS, delete `tun0`, restore IPv6, remove `adb forward`. Driven by the snapshot, so it is correct even if something changed meanwhile.

### 5.3 Watchdog

Every few seconds: is the phone still `device`? is `tun2socks` alive? does the proxy answer? On failure it performs `down` automatically, shows a notification and sets the tray icon to *error*. The PC never stays online-less silently when the cable is pulled.

### 5.4 Crash recovery

`vpnctld` writes `state.json`. On start (and in `ExecStopPost`/`--cleanup`) a leftover state is rolled back before anything else.

## 6. Configuration

`~/.config/vpnctl/config.toml` (system default in `/etc/vpnctl/config.toml`). See [`packaging/config.example.toml`](packaging/config.example.toml).

| Key | Default | Meaning |
|---|---|---|
| `phone_port` | `10808` | Proxy port on the phone |
| `local_port` | `10808` | Forwarded port on the PC |
| `proxy_type` | `socks5` | `socks5` or `http` |
| `dns` | `1.1.1.1, 8.8.8.8` | DNS servers used while connected |
| `dns_over_tls` | `true` | DNS over TCP/TLS (see §11) |
| `disable_ipv6` | `true` | Avoid IPv6 leaks |
| `test_url` | `https://github.com` | Proxy health probe |
| `launch_at_login` | `true` | Tray starts with the session |
| `auto_connect` | `false` | Connect as soon as the phone is detected |
| `helper_at_boot` | `true` | Run `vpnctld` as a systemd service (no password prompt) |

## 7. CLI reference

```bash
vpnctl up      [--phone-port N] [--local-port N] [--proxy socks5|http]
vpnctl down
vpnctl status  [--json]        # state, exit IP, country, uptime
vpnctl test                    # probe only; changes nothing
vpnctl config  get|set <key> [value]
vpnctl logs    [-f]
vpnctl service enable|disable  # systemd helper at boot
vpnctl autostart enable|disable
vpnctl uninstall
vpnctl version
```

Exit codes: `0` ok · `2` phone/adb problem · `3` proxy dead · `4` network setup failed (already rolled back) · `5` permission denied.

## 8. Tray and user interface

### Tray (like PdaNet on Windows)

* Starts silently; only an icon appears.
* Icon states: grey = disconnected, amber = connecting, green = connected, red = error (`assets/tray/`).
* Right-click: **Connect / Disconnect · Open · Settings · Quit**.
* Left-click: open the window. Closing the window hides it; the app stays in the tray.
* Only one instance; starting it again just opens the window.

### Window (local web UI)

* **Dashboard:** big connect button, state, exit IP + country, uptime, live traffic.
* **Settings:** ports, proxy type, DNS, IPv6, *Launch at login*, *Helper at boot*, *Auto-connect*.
* **Logs:** live log view.
* **First-run wizard:** see §9.3.
* Vanilla HTML/CSS/JS only (no framework, no CDN, works offline). Dark theme matching the logo.

Security of the UI server: binds to `127.0.0.1` only, random port, per-session token, strict `Host`/`Origin` checks, no CORS.

> The tray needs a desktop that supports StatusNotifierItem: Cinnamon, KDE, XFCE, MATE and others do. Plain GNOME needs the *AppIndicator* extension. If no tray exists, `vpnctl-gui` simply opens the window.

## 9. Installer and packaging

### 9.1 Deliverables

| File | Purpose |
|---|---|
| `vpnctl_<ver>_amd64.deb` | **Main target.** Fully offline. |
| `vpnctl-installer-<ver>-amd64` | One self-contained file with the graphical wizard, for non-Debian systems. |
| `vpnctl-<ver>-amd64.tar.gz` | Portable archive. |
| `SHA256SUMS` (+ signature) | Integrity. |

### 9.2 What the .deb contains

```
/usr/bin/vpnctl                 /usr/bin/vpnctl-gui
/usr/lib/vpnctl/vpnctld         /usr/lib/vpnctl/tun2socks     /usr/lib/vpnctl/adb
/usr/share/applications/vpnctl.desktop
/usr/share/icons/hicolor/{16..512}x../apps/vpnctl.png   + scalable/apps/vpnctl.svg
/usr/share/polkit-1/actions/org.vpnctl.daemon.policy
/lib/systemd/system/vpnctld.service
/etc/vpnctl/config.toml
```

`postinst` installs files and refreshes icon/desktop caches; it never needs the network. `prerm/postrm` stop the service, roll back network state and, on `purge`, remove config.

### 9.3 Graphical install wizard

A `.deb` has no GUI of its own, so the wizard is the first launch of `vpnctl-gui` (the desktop entry opens it automatically after install; the standalone installer opens the same pages first).

1. **Welcome** (logo, what will be installed).
2. **Startup options (first choices):** ☑ Start vpnctl at login · ☑ Run the helper at boot (no password prompts) · ☐ Auto-connect when the phone is plugged in.
3. **Ports and defaults:** phone port / local port (default 10808).
4. **Phone check:** detects the phone, guides USB-debugging authorisation.
5. **Finish:** the password dialog (polkit) applies system-level choices; then a **Connect** button.

Every choice can be changed later in Settings; the wizard can be re-run with `vpnctl gui --wizard`.

### 9.4 Dependency policy (honest version)

* **Bundled:** `tun2socks`, `adb`, all icons, all web assets. Go binaries are static.
* **Not bundled, assumed present on any desktop Linux:** the kernel `tun` module, systemd (units), polkit (`pkexec` password dialog), systemd-resolved for DNS (with a fallback), and a web browser for the window.
* The `.deb` therefore has an empty `Depends:`; a missing optional piece produces a clear message instead of a failed install.

### 9.5 Uninstall

`vpnctl uninstall` / `apt purge vpnctl` removes binaries, service, polkit policy, desktop and autostart entries, icons and config, and rolls back any active tunnel. (Lesson from the PdaNet installer: nothing is left behind, and no `sudoers` file is ever created.)

## 10. Security model

* One privileged process (`vpnctld`); everything else is unprivileged.
* Root via **polkit/pkexec** only; **no passwordless sudoers**, no setuid binaries.
* The unix socket is authenticated by peer credentials (`SO_PEERCRED`): only the installing user (and group `vpnctl`) may command the helper.
* The helper executes only its own bundled binaries with absolute paths and fixed arguments; ports and DNS values are validated (no shell, no string concatenation).
* The UI server is localhost-only with token + `Origin` check.
* Releases ship with checksums and a signature.
* Privacy note: a *free* VPN config means the config's owner can see unencrypted traffic. Use HTTPS everywhere and avoid sensitive logins over free configs.

## 11. Lessons from field testing

These come from real tests on a Linux Mint PC with an Android phone and are now design requirements:

| Observation | Design consequence |
|---|---|
| The PdaNet+ proxy never appeared; only v2rayNG ports (10808/10809) did. | Target the phone's local SOCKS proxy; do not depend on PdaNet. |
| A script that picked the interface by name (`enp*`) chose the wired port instead of USB. | Never guess by name pattern; vpnctl does not need a tether interface at all (it uses `adb forward`). |
| `127.0.0.1:10808` on the phone is loopback-only, unreachable over tethering. | `adb forward` is the transport. |
| A shell `HTTP_PROXY`/`ALL_PROXY` made a test pass without using the tunnel. | `up` checks for proxy variables and warns; tests are made at socket level, not by trusting `curl` env. |
| TCP worked but DNS (UDP) timed out through SOCKS. | DNS goes over TLS/TCP by default (`dns_over_tls = true`). |
| Running `vpn-up` twice produced `create tun: device or resource busy`. | `up` is idempotent and cleans a half-state first. |
| Ctrl-C on the foreground `tun2socks` left a default route to a dead `tun0`. | Supervised background process + watchdog + rollback. |
| `tun2socks` rejected single-dash long flags in this build. | Use `--flag` form (pinned version, covered by tests). |
| Exit IP changed between countries over time (free configs rotate). | Display exit IP/country live; never assume. |
| IPv6 answers appeared although IPv6 was not tunnelled. | IPv6 is disabled (or fully routed) while connected. |

## 12. Phases

Each phase ends with something runnable and testable.

| Phase | Name | Main deliverables | Done when |
|---|---|---|---|
| **0** | Design + assets | This README (EN/FA), logo, icons, packaging drafts, repo skeleton | Reviewed and approved |
| **1** | Core engine + helper | `internal/{config,adb,tunnel,netcfg,state,watchdog,ipc}`, `vpnctld` | `vpnctld` brings the tunnel up/down; killing it at any point restores the network |
| **2** | CLI | `vpnctl up/down/status/test/config/logs`, exit codes, `--json` | All CLI scenarios pass, including dead proxy, unplugged cable, double `up` |
| **3** | Privilege and services | pkexec launch, polkit policy, systemd unit, socket auth, startup toggles | Password dialog appears; helper-at-boot and autostart can be enabled/disabled |
| **4** | Tray | StatusNotifierItem tray, 4 state icons, menu, notifications, single instance | Works on Cinnamon; right-click Connect connects |
| **5** | Local web UI | Dashboard, Settings, Logs, first-run wizard; embedded, framework-free | Everything doable from the UI that the CLI can do; works offline |
| **6** | Packaging | Bundled `tun2socks`/`adb`, `.deb` (empty Depends), standalone installer, desktop entry + icons, uninstall | Install and use on a fresh VM with **no network**; `purge` leaves nothing |
| **7** | Hardening + release | Network-namespace tests, fuzzed IPC, CI, checksums/signing, docs | Clean CI, tagged release |
| **8** | Later (optional) | Auto-reconnect polish, per-app split tunnel, other distros/arches, translations | — |

Recommended order for review: **1 → 2** first (test on your machine in the terminal), then 3, 4, 5, 6, 7.

## 13. Repository layout

```
vpnctl/
├── README.md  README.fa.md  LICENSE
├── assets/                  logo.svg  icon.svg  banner.svg  png/  tray/
├── cmd/
│   ├── vpnctld/             privileged helper
│   ├── vpnctl/              CLI
│   ├── vpnctl-gui/          tray + local web UI server
│   └── vpnctl-installer/    standalone graphical installer
├── internal/                config adb tunnel netcfg watchdog state ipc tray webui install
├── web/                     embedded UI: index.html  app.js  style.css  wizard
├── bundle/                  tun2socks + adb (pinned versions, checksums)
├── packaging/               vpnctl.desktop  vpnctld.service  polkit policy  config.example.toml  deb/
├── scripts/                 build.sh  make-deb.sh  fetch-bundle.sh
└── docs/
```

## 14. Risks and open decisions

1. **Bundling `adb`.** The adb source is open (Apache-2.0), but the redistribution terms of Google's prebuilt *platform-tools* must be verified. Options: build adb from source, or rely on a distro build. Decided in Phase 6; the code will use an abstraction so the binary can be swapped.
2. **"No graphical framework" and a browser.** The window is a web page, so it needs a web browser installed (every desktop has one). Alternative for headless use: the CLI and an optional terminal UI.
3. **Tray on GNOME.** Needs the AppIndicator extension; the window still works.
4. **`.deb` cannot show a window by itself.** Hence the first-run wizard (§9.3). If you want a prompt *during* `dpkg`, only a text `debconf` question is possible.
5. **DNS backends.** systemd-resolved is the primary path; a fallback for systems without it is planned in Phase 1.
6. **USB debugging prompts.** First connection needs a tap on the phone (*Allow USB debugging*); the wizard guides it.
7. **Free VPN configs are unreliable and not private.** vpnctl cannot fix that; it reports health honestly.

## 15. Licensing

vpnctl: **MIT** (see `LICENSE`). Bundled components keep their own licences: `tun2socks` (MIT), `adb` (Apache-2.0, subject to the point in §14.1). The logo and icons in `assets/` are original and covered by the same MIT licence.
