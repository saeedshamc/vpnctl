(() => {
  const token = document.querySelector('meta[name="vpnctl-token"]')?.content
    || new URLSearchParams(location.search).get("token")
    || localStorage.getItem("vpnctl_token")
    || "";
  if (token) localStorage.setItem("vpnctl_token", token);

  const headers = () => ({
    "Content-Type": "application/json",
    "X-Vpnctl-Token": token,
  });

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      ...opts,
      headers: { ...headers(), ...(opts.headers || {}) },
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  }

  const stateLabel = document.getElementById("state-label");
  const btn = document.getElementById("btn-toggle");
  const exitIP = document.getElementById("exit-ip");
  const uptime = document.getElementById("uptime");
  const phone = document.getElementById("phone");
  const errLine = document.getElementById("err-line");
  const testResult = document.getElementById("test-result");

  function showErr(msg) {
    if (!msg) { errLine.hidden = true; errLine.textContent = ""; return; }
    errLine.hidden = false;
    errLine.textContent = msg;
  }

  function renderStatus(s) {
    const st = s.state || "disconnected";
    stateLabel.textContent = st;
    const connected = st === "connected";
    btn.textContent = connected ? "Disconnect" : "Connect";
    btn.classList.toggle("danger", connected);
    exitIP.textContent = s.exit_ip ? `${s.exit_ip} ${s.exit_country || ""}`.trim() : "—";
    uptime.textContent = s.uptime_sec ? `${s.uptime_sec}s` : "—";
    phone.textContent = s.phone_state || "—";
    showErr(s.last_error || "");
  }

  async function refresh() {
    try {
      const s = await api("/api/status");
      renderStatus(s);
    } catch (e) {
      showErr(String(e.message || e));
    }
  }

  btn.addEventListener("click", async () => {
    btn.disabled = true;
    try {
      const s = await api("/api/status");
      if (s.state === "connected") await api("/api/down", { method: "POST", body: "{}" });
      else await api("/api/up", { method: "POST", body: "{}" });
      await refresh();
    } catch (e) {
      showErr(String(e.message || e));
    } finally {
      btn.disabled = false;
    }
  });

  document.getElementById("btn-test").addEventListener("click", async (ev) => {
    const button = ev.currentTarget;
    button.disabled = true;
    testResult.textContent = "Testing phone proxy…";
    try {
      const result = await api("/api/test", { method: "POST", body: "{}" });
      testResult.textContent = `OK · ${result.latency_ms}ms · phone ${result.phone_state}`;
      if (result.message) testResult.textContent += ` · ${result.message}`;
    } catch (e) {
      testResult.textContent = String(e.message || e);
    } finally {
      button.disabled = false;
    }
  });

  document.querySelectorAll(".nav").forEach((b) => {
    b.addEventListener("click", () => {
      document.querySelectorAll(".nav").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      const view = b.dataset.view;
      ["dash", "settings", "logs", "wizard"].forEach((v) => {
        document.getElementById(`view-${v}`)?.classList.toggle("hidden", v !== view && !(v === "dash" && view === "dash"));
      });
      const initialView = location.hash.slice(1);
      if (initialView === "settings" || initialView === "logs") {
        document.querySelector(`.nav[data-view="${initialView}"]`)?.click();
      }
      document.getElementById("view-dash").classList.toggle("hidden", view !== "dash");
      document.getElementById("view-settings").classList.toggle("hidden", view !== "settings");
      document.getElementById("view-logs").classList.toggle("hidden", view !== "logs");
      if (view === "settings") loadSettings();
      if (view === "logs") loadLogs();
    });
  });

  async function loadSettings() {
    try {
      const cfg = await api("/api/config");
      const form = document.getElementById("settings-form");
      form.phone_port.value = cfg.phone_port;
      form.local_port.value = cfg.local_port;
      form.proxy_type.value = cfg.proxy_type;
      form.dns.value = (cfg.dns || []).join(", ");
      form.test_url.value = cfg.test_url;
      form.log_level.value = cfg.log_level;
      form.dns_over_tls.checked = !!cfg.dns_over_tls;
      form.disable_ipv6.checked = !!cfg.disable_ipv6;
      form.launch_at_login.checked = !!cfg.launch_at_login;
      form.helper_at_boot.checked = !!cfg.helper_at_boot;
      form.auto_connect.checked = !!cfg.auto_connect;
      showErr("");
    } catch (e) {
      showErr(String(e.message || e));
    }
  }

  document.getElementById("settings-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const form = ev.target;
    const body = {
      phone_port: Number(form.phone_port.value),
      local_port: Number(form.local_port.value),
      proxy_type: form.proxy_type.value,
      dns: form.dns.value.split(",").map((s) => s.trim()).filter(Boolean),
      test_url: form.test_url.value,
      log_level: form.log_level.value,
      dns_over_tls: form.dns_over_tls.checked,
      disable_ipv6: form.disable_ipv6.checked,
      launch_at_login: form.launch_at_login.checked,
      helper_at_boot: form.helper_at_boot.checked,
      auto_connect: form.auto_connect.checked,
    };
    try {
      await api("/api/config", { method: "POST", body: JSON.stringify(body) });
      showErr("");
    } catch (e) {
      showErr(String(e.message || e));
    }
  });

  async function loadLogs() {
    const box = document.getElementById("log-box");
    try {
      const data = await api("/api/logs");
      box.textContent = data.text || "";
      box.scrollTop = box.scrollHeight;
    } catch (e) {
      box.textContent = String(e.message || e);
    }
  }

  async function maybeWizard() {
    try {
      const w = await api("/api/wizard");
      if (w.needed) {
        document.querySelectorAll(".view").forEach((v) => v.classList.add("hidden"));
        document.getElementById("view-wizard").classList.remove("hidden");
      }
    } catch (e) {
      showErr(String(e.message || e));
    }
  }

  document.getElementById("wiz-finish").addEventListener("click", async () => {
    const body = {
      launch_at_login: document.getElementById("wiz-login").checked,
      helper_at_boot: document.getElementById("wiz-boot").checked,
      auto_connect: document.getElementById("wiz-auto").checked,
      done: true,
    };
    try {
      await api("/api/wizard", { method: "POST", body: JSON.stringify(body) });
      document.getElementById("view-wizard").classList.add("hidden");
      document.getElementById("view-dash").classList.remove("hidden");
      await refresh();
    } catch (e) {
      showErr(String(e.message || e));
    }
  });

  maybeWizard();
  refresh();
  setInterval(refresh, 3000);
})();
