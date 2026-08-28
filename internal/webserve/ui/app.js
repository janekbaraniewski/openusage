(() => {
  const MIX_COLORS = ["#cba6f7", "#89b4fa", "#94e2d5", "#fab387", "#f38ba8", "#f9e2af", "#a6e3a1", "#b4befe"];
  const COST_KEYS = ["today_cost", "today_api_cost", "usage_daily", "7d_api_cost", "usage_weekly", "30d_api_cost", "usage_monthly", "plan_spend", "plan_total_spend_usd", "all_time_api_cost", "credit_balance"];
  const TOKEN_KEYS = ["today_input_tokens", "today_output_tokens", "billing_input_tokens", "billing_output_tokens", "7d_input_tokens", "7d_output_tokens"];

  const state = {
    envelope: null,
    view: "overview",
    selected: null,
    token: sessionStorage.getItem("ou-serve-token") || "",
    theme: localStorage.getItem("ou-serve-theme") || "dark",
  };

  const $ = (id) => document.getElementById(id);
  const el = (tag, attrs = {}, children = []) => {
    const node = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") node.className = v;
      else if (k === "text") node.textContent = v;
      else if (k.startsWith("on") && typeof v === "function") node.addEventListener(k.slice(2), v);
      else if (v != null) node.setAttribute(k, v);
    }
    for (const child of children) node.append(child);
    return node;
  };

  function applyTheme() {
    document.documentElement.dataset.theme = state.theme;
    $("theme-toggle").textContent = state.theme === "dark" ? "Light" : "Dark";
  }

  function catalogName(id) {
    const hit = (state.envelope?.catalog || []).find((c) => c.id === id);
    return hit?.name || id;
  }

  function metricUsed(metric) {
    if (!metric) return null;
    if (typeof metric.used === "number") return metric.used;
    if (typeof metric.limit === "number" && typeof metric.remaining === "number") {
      return metric.limit - metric.remaining;
    }
    return null;
  }

  function pickMetric(snap, keys) {
    const metrics = snap.metrics || {};
    for (const key of keys) {
      if (metrics[key]) return { key, metric: metrics[key], value: metricUsed(metrics[key]) };
    }
    return null;
  }

  function costOf(snap) {
    const picked = pickMetric(snap, COST_KEYS);
    return picked?.value ?? 0;
  }

  function tokensOf(snap) {
    const metrics = snap.metrics || {};
    let total = 0;
    let found = false;
    for (const key of TOKEN_KEYS) {
      const value = metricUsed(metrics[key]);
      if (typeof value === "number") {
        total += value;
        found = true;
      }
    }
    return found ? total : 0;
  }

  function remainingRatio(metric) {
    if (!metric) return null;
    if (typeof metric.limit === "number" && metric.limit > 0 && typeof metric.remaining === "number") {
      return metric.remaining / metric.limit;
    }
    if (typeof metric.limit === "number" && metric.limit > 0 && typeof metric.used === "number") {
      return Math.max(0, (metric.limit - metric.used) / metric.limit);
    }
    if (metric.unit === "%" && typeof metric.used === "number") {
      return Math.max(0, 1 - metric.used / 100);
    }
    return null;
  }

  function gaugeMetric(snap) {
    const metrics = snap.metrics || {};
    const preferred = ["usage_five_hour", "usage_seven_day", "credit_balance", "plan_spend", "spend_limit", "premium_requests"];
    for (const key of preferred) {
      if (metrics[key] && remainingRatio(metrics[key]) != null) return { key, metric: metrics[key] };
    }
    for (const [key, metric] of Object.entries(metrics)) {
      if (remainingRatio(metric) != null) return { key, metric };
    }
    return null;
  }

  function money(n) {
    if (!Number.isFinite(n)) return "—";
    if (Math.abs(n) >= 1000) return `$${(n / 1000).toFixed(1)}k`;
    return `$${n.toFixed(n >= 100 ? 0 : 2)}`;
  }

  function compact(n) {
    if (!Number.isFinite(n)) return "—";
    const abs = Math.abs(n);
    if (abs >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
    if (abs >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
    if (abs >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
    if (Number.isInteger(n)) return String(n);
    return n.toFixed(2);
  }

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

  function statusLabel(status) {
    return (status || "UNKNOWN").replaceAll("_", " ");
  }

  function snapKey(snap) {
    return `${snap.provider_id}:${snap.account_id}`;
  }

  function headers() {
    const h = {};
    if (state.token) h.Authorization = `Bearer ${state.token}`;
    return h;
  }

  async function load() {
    const res = await fetch("/api/v1/snapshots", { headers: headers() });
    if (res.status === 401) {
      $("token-modal").hidden = false;
      $("token-error").hidden = false;
      $("token-error").textContent = state.token ? "Invalid token" : "Token required";
      return;
    }
    if (!res.ok) throw new Error(`snapshots ${res.status}`);
    $("token-modal").hidden = true;
    state.envelope = await res.json();
    render();
  }

  function render() {
    const env = state.envelope;
    if (!env) return;
    $("source-pill").textContent = `${env.source} · ${env.time_window || ""}`.trim();
    const snaps = env.snapshots || [];
    $("empty-state").hidden = snaps.length > 0;
    $("overview-grid").hidden = state.view !== "overview" || snaps.length === 0;
    $("kpis").hidden = snaps.length === 0;
    renderKpis(snaps);
    renderSpend(snaps);
    renderMix(snaps);
    renderCards(snaps);
  }

  function renderKpis(snaps) {
    const spend = snaps.reduce((sum, s) => sum + costOf(s), 0);
    const tokens = snaps.reduce((sum, s) => sum + tokensOf(s), 0);
    const healthy = snaps.filter((s) => s.status === "OK").length;
    const items = [
      ["Spend in view", money(spend)],
      ["Tokens", compact(tokens)],
      ["Providers", String(snaps.length)],
      ["Healthy", `${healthy}/${snaps.length}`],
    ];
    const root = $("kpis");
    root.replaceChildren();
    for (const [label, value] of items) {
      root.append(el("article", { class: "kpi" }, [
        el("p", { class: "label", text: label }),
        el("p", { class: "value", text: value }),
      ]));
    }
  }

  function dailyCostSeries(snaps) {
    const byDate = new Map();
    for (const snap of snaps) {
      const series = snap.daily_series?.cost || snap.daily_series?.analytics_cost || [];
      for (const pt of series) {
        byDate.set(pt.date, (byDate.get(pt.date) || 0) + (pt.value || 0));
      }
    }
    return [...byDate.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }

  function renderSpend(snaps) {
    const root = $("spend-chart");
    const series = dailyCostSeries(snaps).slice(-14);
    root.replaceChildren();
    if (!series.length) {
      root.append(el("p", { class: "muted", text: "No daily series in these snapshots." }));
      return;
    }
    const max = Math.max(...series.map(([, v]) => v), 1);
    const bars = el("div", { class: "bar-row" });
    for (const [date, value] of series) {
      const bar = el("div", { class: "bar", title: `${date}: ${money(value)}` });
      bar.style.height = `${Math.max(6, (value / max) * 100)}%`;
      bars.append(bar);
    }
    root.append(bars);
    root.append(el("div", { class: "bar-axis" }, [
      el("span", { text: series[0][0].slice(5) }),
      el("span", { text: series[series.length - 1][0].slice(5) }),
    ]));
  }

  function renderMix(snaps) {
    const root = $("model-mix");
    const totals = new Map();
    for (const snap of snaps) {
      for (const rec of snap.model_usage || []) {
        const name = rec.canonical || rec.raw_model_id || "unknown";
        totals.set(name, (totals.get(name) || 0) + (rec.cost_usd || 0));
      }
    }
    const rows = [...totals.entries()].filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]).slice(0, 8);
    root.replaceChildren();
    if (!rows.length) {
      root.append(el("p", { class: "muted", text: "No per-model cost yet." }));
      return;
    }
    const sum = rows.reduce((s, [, v]) => s + v, 0) || 1;
    rows.forEach(([name, value], i) => {
      const color = MIX_COLORS[i % MIX_COLORS.length];
      const track = el("div", { class: "mix-track" }, [el("span")]);
      track.firstChild.style.width = `${(value / sum) * 100}%`;
      track.firstChild.style.background = color;
      root.append(el("div", { class: "mix-row" }, [
        el("span", { class: "mix-swatch", style: `background:${color}` }),
        el("span", { class: "mix-label", text: name }),
        track,
        el("span", { class: "mix-value", text: money(value) }),
      ]));
    });
  }

  function renderCards(snaps) {
    const root = $("provider-grid");
    $("provider-count").textContent = `${snaps.length} account${snaps.length === 1 ? "" : "s"}`;
    root.replaceChildren();
    for (const snap of snaps) {
      const gauge = gaugeMetric(snap);
      const ratio = gauge ? remainingRatio(gauge.metric) : null;
      const usedPct = ratio == null ? null : Math.round((1 - ratio) * 100);
      const gaugeEl = el("div", { class: `gauge ${usedPct >= 90 ? "crit" : usedPct >= 70 ? "warn" : ""}` });
      const fill = el("span");
      fill.style.width = `${usedPct == null ? 0 : Math.min(100, usedPct)}%`;
      gaugeEl.append(fill);
      const card = el("article", { class: "card", onclick: () => openDrawer(snap) }, [
        el("div", { class: "card-head" }, [
          el("div", {}, [
            el("h3", { text: catalogName(snap.provider_id) }),
            el("p", { class: "account", text: snap.account_id }),
          ]),
          el("span", { class: `pill ${statusClass(snap.status)}`, text: statusLabel(snap.status) }),
        ]),
        el("p", { class: "hero", text: snap.message || money(costOf(snap)) }),
        gaugeEl,
      ]);
      root.append(card);
    }
  }

  function openDrawer(snap) {
    state.selected = snapKey(snap);
    $("drawer").hidden = false;
    $("drawer-title").textContent = catalogName(snap.provider_id);
    $("drawer-sub").textContent = `${snap.account_id} · ${snap.message || ""}`;
    const status = $("drawer-status");
    status.textContent = statusLabel(snap.status);
    status.className = `pill ${statusClass(snap.status)}`;
    const body = $("drawer-body");
    body.replaceChildren();

    const metrics = Object.entries(snap.metrics || {}).sort(([a], [b]) => a.localeCompare(b));
    const table = el("table", { class: "metric-table" });
    table.append(el("thead", {}, [el("tr", {}, [
      el("th", { text: "Metric" }),
      el("th", { text: "Used" }),
      el("th", { text: "Limit" }),
    ])]));
    const tbody = el("tbody");
    for (const [key, metric] of metrics.slice(0, 40)) {
      tbody.append(el("tr", {}, [
        el("td", { text: key }),
        el("td", { text: formatMetric(metric.used, metric.unit) }),
        el("td", { text: formatMetric(metric.limit, metric.unit) }),
      ]));
    }
    table.append(tbody);
    body.append(el("h3", { text: "Metrics" }), table);

    if ((snap.model_usage || []).length) {
      const models = el("table", { class: "metric-table" });
      models.append(el("thead", {}, [el("tr", {}, [el("th", { text: "Model" }), el("th", { text: "Cost" })])]));
      const mb = el("tbody");
      for (const rec of snap.model_usage) {
        mb.append(el("tr", {}, [
          el("td", { text: rec.canonical || rec.raw_model_id }),
          el("td", { text: rec.cost_usd != null ? money(rec.cost_usd) : "—" }),
        ]));
      }
      models.append(mb);
      body.append(el("h3", { text: "Models" }), models);
    }
  }

  function formatMetric(value, unit) {
    if (typeof value !== "number") return "—";
    if (unit === "USD" || unit === "USD/h") return money(value);
    if (unit === "tokens") return compact(value);
    return `${compact(value)}${unit ? " " + unit : ""}`;
  }

  function closeDrawer() {
    $("drawer").hidden = true;
    state.selected = null;
  }

  $("theme-toggle").addEventListener("click", () => {
    state.theme = state.theme === "dark" ? "light" : "dark";
    localStorage.setItem("ou-serve-theme", state.theme);
    applyTheme();
  });
  $("refresh-btn").addEventListener("click", () => load().catch((err) => {
    $("source-pill").textContent = "error";
    console.error(err);
  }));
  document.querySelectorAll(".tab").forEach((btn) => {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach((b) => b.classList.toggle("is-active", b === btn));
      state.view = btn.dataset.view;
      $("overview-grid").hidden = state.view !== "overview";
      $("kpis").hidden = state.view !== "overview";
    });
  });
  $("drawer").addEventListener("click", (ev) => {
    if (ev.target.closest("[data-close]")) closeDrawer();
  });
  $("token-form").addEventListener("submit", (ev) => {
    ev.preventDefault();
    state.token = $("token-input").value.trim();
    sessionStorage.setItem("ou-serve-token", state.token);
    load().catch(console.error);
  });

  applyTheme();
  load().catch((err) => {
    $("source-pill").textContent = "error";
    $("empty-state").hidden = false;
    console.error(err);
  });
  let refreshTimer = 0;
  const tick = () => {
    const seconds = Math.max(5, state.envelope?.refresh_interval_seconds || 30);
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => {
      if (!document.hidden) {
        load().catch(() => {});
      }
      tick();
    }, seconds * 1000);
  };
  tick();
})();
