(() => {
  "use strict";

  const SPINNER = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

  const state = {
    envelope: null,
    views: [],
    selected: 0,
    filter: "",
    token: sessionStorage.getItem("ou-serve-token") || "",
    themeOverride: localStorage.getItem("ou-serve-theme-override") || "",
    loading: true,
    refreshing: false,
    spinnerFrame: 0,
    error: null,
  };

  const $ = (id) => document.getElementById(id);

  const el = (tag, attrs = {}, children = []) => {
    const node = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") node.className = v;
      else if (k === "text") node.textContent = v;
      else if (k === "html") node.innerHTML = v;
      else if (k.startsWith("on") && typeof v === "function") node.addEventListener(k.slice(2).toLowerCase(), v);
      else if (v != null) node.setAttribute(k, v);
    }
    for (const child of children) {
      if (child != null) node.append(child);
    }
    return node;
  };

  function statusClass(status) {
    switch (status) {
      case "OK": return "ok";
      case "NEAR_LIMIT": return "warn";
      case "LIMITED":
      case "ERROR": return "crit";
      case "AUTH_REQUIRED": return "auth";
      default: return "";
    }
  }

  function renderMiniGauge(percent, width = 8) {
    if (percent < 0 || !Number.isFinite(percent)) return "";
    const pct = Math.min(100, Math.max(0, percent));
    const filled = Math.round((pct / 100) * width);
    return "█".repeat(filled) + "░".repeat(Math.max(0, width - filled));
  }

  function applyThemeTokens(tokens, override) {
    const root = document.documentElement;
    if (override === "light") {
      root.dataset.theme = "light";
      return;
    }
    if (!tokens || !tokens.base) {
      root.dataset.theme = "deep-space";
      return;
    }
    root.dataset.theme = "dynamic";
    const map = {
      "--base": tokens.base,
      "--mantle": tokens.mantle,
      "--surface0": tokens.surface0,
      "--surface1": tokens.surface1,
      "--surface2": tokens.surface2,
      "--text": tokens.text,
      "--subtext": tokens.subtext,
      "--dim": tokens.dim,
      "--accent": tokens.accent,
      "--blue": tokens.blue,
      "--sapphire": tokens.sapphire,
      "--green": tokens.green,
      "--yellow": tokens.yellow,
      "--red": tokens.red,
      "--peach": tokens.peach,
      "--teal": tokens.teal,
      "--lavender": tokens.lavender,
      "--mauve": tokens.mauve,
      "--ok": tokens.green,
      "--warn": tokens.yellow,
      "--crit": tokens.red,
      "--auth": tokens.peach,
    };
    for (const [key, val] of Object.entries(map)) {
      if (val) root.style.setProperty(key, val);
    }
  }

  function cycleThemeOverride() {
    const order = ["", "light"];
    const idx = order.indexOf(state.themeOverride);
    state.themeOverride = order[(idx + 1) % order.length];
    localStorage.setItem("ou-serve-theme-override", state.themeOverride);
    applyThemeTokens(state.envelope?.theme_tokens, state.themeOverride);
  }

  function filteredViews() {
    const q = state.filter.trim().toLowerCase();
    if (!q) return state.views;
    return state.views.filter((v) =>
      v.provider_id.toLowerCase().includes(q)
      || v.provider_name.toLowerCase().includes(q)
      || v.account_id.toLowerCase().includes(q)
      || (v.summary || "").toLowerCase().includes(q),
    );
  }

  function headers() {
    const h = {};
    if (state.token) h.Authorization = `Bearer ${state.token}`;
    return h;
  }

  async function load() {
    state.refreshing = true;
    renderHeader();
    try {
      const res = await fetch("/api/v1/snapshots", { headers: headers() });
      if (res.status === 401) {
        $("token-modal").hidden = false;
        $("token-error").hidden = false;
        $("token-error").textContent = state.token ? "Invalid token" : "Token required";
        state.refreshing = false;
        return;
      }
      if (!res.ok) throw new Error(`snapshots ${res.status}`);
      $("token-modal").hidden = true;
      state.envelope = await res.json();
      state.views = state.envelope.views || [];
      state.error = null;
      applyThemeTokens(state.envelope.theme_tokens, state.themeOverride);
      const visible = filteredViews();
      if (state.selected >= visible.length) state.selected = Math.max(0, visible.length - 1);
      showDashboard(state.views.length > 0);
      render();
    } catch (err) {
      state.error = String(err);
      showDashboard(false);
      $("empty-state").hidden = false;
      $("splash").hidden = true;
      $("app").hidden = true;
    } finally {
      state.loading = false;
      state.refreshing = false;
      renderHeader();
      renderFooter();
    }
  }

  function showDashboard(hasData) {
    $("splash").hidden = true;
    $("empty-state").hidden = hasData;
    $("app").hidden = !hasData;
  }

  function countStatuses(views) {
    let ok = 0;
    let warn = 0;
    let err = 0;
    for (const v of views) {
      switch (v.status) {
        case "OK": ok++; break;
        case "NEAR_LIMIT": warn++; break;
        case "LIMITED":
        case "ERROR": err++; break;
        default: break;
      }
    }
    return { ok, warn, err };
  }

  function renderHeader() {
    const env = state.envelope;
    const views = state.views;
    const counts = countStatuses(views);
    const root = $("header");
    root.replaceChildren();

    const spinner = state.refreshing
      ? el("span", { class: "spinner", text: SPINNER[state.spinnerFrame % SPINNER.length] })
      : null;

    const countParts = [];
    if (counts.ok) countParts.push(el("span", { class: "ok", text: `${counts.ok}●` }));
    if (counts.warn) countParts.push(el("span", { class: "warn", text: `${counts.warn}◐` }));
    if (counts.err) countParts.push(el("span", { class: "crit", text: `${counts.err}✗` }));

    const filterLabel = state.filter ? " (filtered)" : "";
    const line = el("div", { class: "dash-header-line" }, [
      el("span", { class: "brand-bolt", text: "⚡" }),
      el("span", { class: "brand-name", text: "OpenUsage" }),
      el("span", { class: "header-counts" }, countParts),
      spinner,
      el("span", {
        class: "header-meta",
        text: env
          ? `⊞ ${views.length} provider${views.length === 1 ? "" : "s"}${filterLabel} · ${env.time_window || ""} · ${env.source}`
          : "connecting…",
      }),
    ]);

    root.append(line, el("div", { class: "header-sep", text: "━".repeat(120) }));
  }

  function renderFooter() {
    const env = state.envelope;
    const root = $("footer");
    root.replaceChildren();

    const line = el("div", { class: "footer-line" }, [
      el("span", {}, [el("kbd", { text: "↑" }), document.createTextNode(" "), el("kbd", { text: "↓" }), document.createTextNode(" navigate")]),
      el("span", { text: "·" }),
      el("span", {}, [el("kbd", { text: "/" }), document.createTextNode(" filter")]),
      el("span", { text: "·" }),
      el("span", {}, [el("kbd", { text: "r" }), document.createTextNode(" refresh")]),
      el("span", { text: "·" }),
      el("span", {}, [el("kbd", { text: "t" }), document.createTextNode(" theme")]),
      el("span", { style: "flex:1" }),
      el("span", {
        class: "dim",
        text: env
          ? `${env.theme || ""} · ${env.usage_mode || "remaining"} · refresh ${env.refresh_interval_seconds || 30}s · ${new Date(env.generated_at).toLocaleTimeString()}`
          : state.error || "offline",
      }),
    ]);

    root.append(el("div", { class: "footer-sep", text: "━".repeat(120) }), line);
  }

  function renderNav() {
    const views = filteredViews();
    const root = $("nav");
    root.replaceChildren();
    if (!views.length) {
      root.append(el("p", { class: "dim", style: "padding:12px", text: state.filter ? "No matches." : "No providers." }));
      return;
    }

    const selectedView = views[state.selected];
    const selectedProvider = selectedView?.provider_id;
    let lastProvider = "";

    views.forEach((view, index) => {
      const pID = view.provider_id;
      const isFirst = pID !== lastProvider;
      lastProvider = pID;

      if (isFirst) {
        const groupCount = views.filter((v) => v.provider_id === pID).length;
        const active = pID === selectedProvider;
        root.append(el("div", {
          class: `nav-group-header ${active ? "active" : "inactive"}`,
          style: active ? `color:${view.accent_color}` : "",
          text: `${pID.toUpperCase()} (${groupCount})`,
        }));
      }

      const inGroup = pID === selectedProvider;
      const selected = index === state.selected;
      const mini = view.gauge_percent >= 0 ? renderMiniGauge(view.gauge_percent) : "";

      root.append(el("button", {
        class: `nav-item${selected ? " selected" : ""}${inGroup && !selected ? " in-group" : ""}`,
        type: "button",
        style: `--item-accent:${view.accent_color}`,
        onclick: () => { state.selected = index; render(); },
      }, [
        el("div", { class: "nav-row1" }, [
          el("span", { class: `nav-status ${statusClass(view.status)}`, text: view.status_icon }),
          el("span", { class: "nav-name", text: view.account_id }),
          el("span", { class: `nav-badge ${statusClass(view.status)}`, text: view.status_badge }),
        ]),
        el("div", { class: "nav-row2" }, [
          el("span", { class: "nav-summary", text: view.summary || "—" }),
          mini ? el("span", { class: "nav-mini-gauge dim", text: mini }) : null,
        ]),
        el("hr", { class: "nav-sep" }),
      ]));
    });

    root.querySelector(".nav-item.selected")?.scrollIntoView({ block: "nearest" });
  }

  function renderSparkline(points) {
    if (!points?.length) return null;
    const series = points.slice(-14);
    const max = Math.max(...series.map((p) => p.value || 0), 0.01);
    const row = el("div", { class: "spark-row" });
    for (const pt of series) {
      const bar = el("span", { class: "spark-bar", title: `${pt.date}: $${(pt.value || 0).toFixed(2)}` });
      bar.style.height = `${Math.max(4, ((pt.value || 0) / max) * 100)}%`;
      row.append(bar);
    }
    return el("div", { class: "spark-wrap" }, [
      el("div", { class: "tile-section-title", text: "Daily spend" }),
      row,
    ]);
  }

  function renderPanel() {
    const views = filteredViews();
    const root = $("panel");
    root.replaceChildren();
    if (!views.length) return;

    const view = views[state.selected];
    const tile = el("article", { class: "tile", style: `--tile-accent:${view.accent_color}` });

    tile.append(el("div", { class: "tile-header" }, [
      el("span", { class: statusClass(view.status), text: view.status_icon }),
      el("span", { class: "tile-title", text: view.account_id }),
      el("span", { class: `nav-badge ${statusClass(view.status)}`, text: view.status_badge }),
      el("span", { class: "dim", text: view.provider_name }),
    ]));
    tile.append(el("hr", { class: "tile-accent-sep" }));

    if (view.tag_label || view.tag_emoji) {
      tile.append(el("div", { class: "tile-tagline" }, [
        el("span", { text: `${view.tag_emoji || ""} ${view.tag_label || ""}`.trim() }),
        view.detail ? el("span", { class: "dim", text: view.detail }) : null,
      ]));
    }

    if (view.message) {
      tile.append(el("div", { class: "tile-hero", text: view.message }));
    } else if (view.summary) {
      tile.append(el("div", { class: "tile-hero", text: view.summary }));
    }

    if (view.resets?.length) {
      tile.append(el("div", { class: "reset-pills" }, view.resets.map((p) =>
        el("span", { class: `reset-pill${p.urgent ? " urgent" : ""}`, html: `◷ ${p.label} <strong>${p.duration}</strong>` }),
      )));
    }

    const spark = renderSparkline(view.daily_cost);
    if (spark) tile.append(spark);

    if (view.tile_lines?.length) {
      tile.append(el("pre", { class: "tile-pre", text: view.tile_lines.join("\n") }));
    }

    for (const sec of view.detail_sections || []) {
      if (!sec.lines?.length) continue;
      const title = [sec.icon, sec.title].filter(Boolean).join(" ");
      if (title.trim()) tile.append(el("div", { class: "tile-section-title", text: title }));
      const block = el("pre", { class: "detail-pre" });
      block.textContent = sec.lines.join("\n");
      tile.append(block);
    }

    if (view.timestamp) {
      const ts = new Date(view.timestamp);
      const diff = Date.now() - ts.getTime();
      const age = diff > 60000
        ? `${Math.floor(diff / 60000)}m ago`
        : ts.toLocaleTimeString();
      tile.append(el("div", { class: "tile-footer", text: `⏱ ${age}` }));
    }

    root.append(tile);
  }

  function render() {
    if (!state.envelope) return;
    renderHeader();
    renderNav();
    renderPanel();
    renderFooter();
  }

  function moveSelection(delta) {
    const views = filteredViews();
    if (!views.length) return;
    state.selected = Math.max(0, Math.min(views.length - 1, state.selected + delta));
    render();
  }

  function startFilter() {
    const q = window.prompt("Filter providers", state.filter);
    if (q == null) return;
    state.filter = q;
    state.selected = 0;
    render();
  }

  $("token-form").addEventListener("submit", (ev) => {
    ev.preventDefault();
    state.token = $("token-input").value.trim();
    sessionStorage.setItem("ou-serve-token", state.token);
    load().catch(console.error);
  });

  document.addEventListener("keydown", (ev) => {
    if ($("token-modal").hidden === false) return;
    if (ev.target.matches("input, textarea")) return;
    switch (ev.key) {
      case "ArrowUp":
      case "k":
        ev.preventDefault();
        moveSelection(-1);
        break;
      case "ArrowDown":
      case "j":
        ev.preventDefault();
        moveSelection(1);
        break;
      case "/":
        ev.preventDefault();
        startFilter();
        break;
      case "r":
      case "R":
        ev.preventDefault();
        load().catch(console.error);
        break;
      case "t":
      case "T":
        ev.preventDefault();
        cycleThemeOverride();
        break;
      default:
        break;
    }
  });

  setInterval(() => {
    if (!state.refreshing) return;
    state.spinnerFrame += 1;
    const spin = document.querySelector(".spinner");
    if (spin) spin.textContent = SPINNER[state.spinnerFrame % SPINNER.length];
  }, 80);

  let refreshTimer = 0;
  const scheduleRefresh = () => {
    clearTimeout(refreshTimer);
    const seconds = Math.max(5, state.envelope?.refresh_interval_seconds || 30);
    refreshTimer = setTimeout(() => {
      if (!document.hidden) load().catch(() => {});
      scheduleRefresh();
    }, seconds * 1000);
  };

  load().then(scheduleRefresh).catch((err) => {
    console.error(err);
    $("splash").hidden = true;
    $("empty-state").hidden = false;
  });
})();
