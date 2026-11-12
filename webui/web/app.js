"use strict";
const $ = (s, el = document) => el.querySelector(s);
const SR = 44100;
const TYPES = [["PK", "Peaking"], ["LS", "Low shelf"], ["HS", "High shelf"], ["LP", "Low pass"], ["HP", "High pass"], ["NO", "Notch"]];
const hasGain = t => t === "PK" || t === "LS" || t === "HS";

let presets = [];
let savedEQ = null;
let eq = { enabled: false, preset: "flat", preamp: 0, auto_preamp: true, filters: [] };

// ---------------------------------------------------------------- helpers
function toast(msg, isError) {
  const t = $("#toast");
  t.textContent = msg;
  t.className = isError ? "error" : "";
  t.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => (t.hidden = true), isError ? 6000 : 3000);
}

async function api(path, body) {
  const opts = body === undefined ? {} : {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  };
  const r = await fetch("api/" + path, opts);
  const ct = r.headers.get("Content-Type") || "";
  const data = ct.includes("json") ? await r.json() : await r.text();
  if (!r.ok) throw new Error((data && data.error) || data || r.statusText);
  return data;
}

const clone = o => JSON.parse(JSON.stringify(o));
const fmtTime = ms => { const s = Math.max(0, Math.floor(ms / 1000)); return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`; };

// ---------------------------------------------------------------- biquad math (RBJ cookbook)
function coeffs(f) {
  const w0 = 2 * Math.PI * f.freq / SR, cw = Math.cos(w0), sw = Math.sin(w0);
  const alpha = sw / (2 * f.q), A = Math.pow(10, f.gain / 40), sa = 2 * Math.sqrt(A) * alpha;
  switch (f.type) {
    case "PK": return [1 + alpha * A, -2 * cw, 1 - alpha * A, 1 + alpha / A, -2 * cw, 1 - alpha / A];
    case "LS": return [A * ((A + 1) - (A - 1) * cw + sa), 2 * A * ((A - 1) - (A + 1) * cw), A * ((A + 1) - (A - 1) * cw - sa),
      (A + 1) + (A - 1) * cw + sa, -2 * ((A - 1) + (A + 1) * cw), (A + 1) + (A - 1) * cw - sa];
    case "HS": return [A * ((A + 1) + (A - 1) * cw + sa), -2 * A * ((A - 1) + (A + 1) * cw), A * ((A + 1) + (A - 1) * cw - sa),
      (A + 1) - (A - 1) * cw + sa, 2 * ((A - 1) - (A + 1) * cw), (A + 1) - (A - 1) * cw - sa];
    case "LP": return [(1 - cw) / 2, 1 - cw, (1 - cw) / 2, 1 + alpha, -2 * cw, 1 - alpha];
    case "HP": return [(1 + cw) / 2, -(1 + cw), (1 + cw) / 2, 1 + alpha, -2 * cw, 1 - alpha];
    case "NO": return [1, -2 * cw, 1, 1 + alpha, -2 * cw, 1 - alpha];
  }
  return [1, 0, 0, 1, 0, 0];
}
function magDB(c, freq) {
  const w = 2 * Math.PI * freq / SR, c1 = Math.cos(w), c2 = Math.cos(2 * w), s1 = Math.sin(w), s2 = Math.sin(2 * w);
  const nr = c[0] + c[1] * c1 + c[2] * c2, ni = -c[1] * s1 - c[2] * s2;
  const dr = c[3] + c[4] * c1 + c[5] * c2, di = -c[4] * s1 - c[5] * s2;
  return 10 * Math.log10((nr * nr + ni * ni) / (dr * dr + di * di));
}
const validFilter = f => f.freq >= 10 && f.freq < SR / 2 - 50 && f.q >= 0.05 && f.q <= 30 && Math.abs(f.gain) <= 30;
function response(filters, freqs) {
  const cs = filters.filter(f => f.enabled && validFilter(f)).map(coeffs);
  return freqs.map(fr => cs.reduce((s, c) => s + magDB(c, fr), 0));
}
const logFreqs = n => Array.from({ length: n }, (_, i) => 20 * Math.pow(1000, i / (n - 1)));
function autoPreamp(filters) {
  const peak = Math.max(...response(filters, logFreqs(400)));
  return peak > 0 ? -Math.ceil(peak * 10) / 10 : 0;
}

// ---------------------------------------------------------------- plot
function drawPlot() {
  const cv = $("#eq-plot"), ctx = cv.getContext("2d");
  const dpr = window.devicePixelRatio || 1, cssW = cv.clientWidth || 900, cssH = Math.round(cssW * 0.29);
  if (cv.width !== Math.round(cssW * dpr)) { cv.width = Math.round(cssW * dpr); cv.height = Math.round(cssH * dpr); }
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  const W = cssW, H = cssH, padL = 34, padB = 18, range = 15;
  ctx.clearRect(0, 0, W, H);
  const x = f => padL + (Math.log10(f / 20) / 3) * (W - padL - 6);
  const y = db => 6 + (range - db) / (2 * range) * (H - padB - 12);
  ctx.font = "11px system-ui"; ctx.fillStyle = "#777"; ctx.strokeStyle = "#2a2a2a"; ctx.lineWidth = 1;
  for (const f of [20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000]) {
    ctx.beginPath(); ctx.moveTo(x(f), y(range)); ctx.lineTo(x(f), y(-range)); ctx.stroke();
    ctx.fillText(f >= 1000 ? f / 1000 + "k" : f, x(f) - 8, H - 4);
  }
  for (let db = -range; db <= range; db += 5) {
    ctx.strokeStyle = db === 0 ? "#444" : "#2a2a2a";
    ctx.beginPath(); ctx.moveTo(padL, y(db)); ctx.lineTo(W - 6, y(db)); ctx.stroke();
    ctx.fillText((db > 0 ? "+" : "") + db, 4, y(db) + 4);
  }
  const freqs = logFreqs(300);
  const resp = response(eq.filters, freqs);
  const pre = eq.auto_preamp ? autoPreamp(eq.filters) : eq.preamp;
  const line = (vals, color, width) => {
    ctx.strokeStyle = color; ctx.lineWidth = width; ctx.beginPath();
    vals.forEach((v, i) => { const yy = y(Math.max(-range, Math.min(range, v))); i ? ctx.lineTo(x(freqs[i]), yy) : ctx.moveTo(x(freqs[i]), yy); });
    ctx.stroke();
  };
  const on = eq.enabled;
  line(resp, on ? "#1db95466" : "#55555566", 1.5);                 // filters only
  line(resp.map(v => v + pre), on ? "#1ed760" : "#777", 2.5);     // incl. preamp
  ctx.fillStyle = on ? "#9a9a9a" : "#e3b341";
  ctx.fillText(on ? `preamp ${pre.toFixed(1)} dB` : "EQ disabled (bypassed)", W - 140, 18);
}

// ---------------------------------------------------------------- EQ UI
function renderEQ() {
  $("#eq-enabled").checked = eq.enabled;
  $("#eq-autopreamp").checked = eq.auto_preamp;
  $("#eq-preamp").disabled = eq.auto_preamp;
  $("#eq-preamp").value = (eq.auto_preamp ? autoPreamp(eq.filters) : eq.preamp).toFixed(1);
  const sel = $("#eq-preset");
  if (!presets.some(p => p.id === eq.preset) && eq.preset !== "custom") eq.preset = "custom";
  sel.value = eq.preset;

  const tb = $("#eq-table tbody");
  tb.textContent = "";
  eq.filters.forEach((f, i) => {
    const tr = document.createElement("tr");
    const td = el => { const c = document.createElement("td"); c.append(el); tr.append(c); return el; };
    const inp = (type, attrs) => { const e = document.createElement("input"); e.type = type; Object.assign(e, attrs); return e; };

    const en = td(inp("checkbox", { checked: f.enabled }));
    en.onchange = () => { f.enabled = en.checked; changed(); };

    const ty = document.createElement("select");
    TYPES.forEach(([v, n]) => ty.add(new Option(n, v, false, v === f.type)));
    td(ty).onchange = () => { f.type = ty.value; if (!hasGain(f.type)) f.gain = 0; changed(true); };

    const fr = td(inp("number", { min: 10, max: 22000, step: "any", value: +f.freq.toFixed(1) }));
    fr.oninput = () => { f.freq = +fr.value; changed(); };

    const gn = td(inp("number", { min: -30, max: 30, step: 0.1, value: f.gain.toFixed(1), disabled: !hasGain(f.type) }));
    const gs = td(inp("range", { min: -15, max: 15, step: 0.1, value: f.gain, disabled: !hasGain(f.type) }));
    gn.oninput = () => { f.gain = +gn.value; gs.value = f.gain; changed(); };
    gs.oninput = () => { f.gain = +gs.value; gn.value = f.gain.toFixed(1); changed(); };

    const q = td(inp("number", { min: 0.05, max: 30, step: 0.01, value: f.q }));
    q.oninput = () => { f.q = +q.value; changed(); };

    const del = document.createElement("button");
    del.type = "button"; del.className = "secondary small"; del.textContent = "✕"; del.title = "Remove";
    td(del).onclick = () => { eq.filters.splice(i, 1); changed(true); };
    tb.append(tr);
  });
  drawPlot();
}

let liveTimer = null;
function changed(rerender) {
  eq.preset = "custom";
  $("#eq-preset").value = "custom";
  if (eq.auto_preamp) $("#eq-preamp").value = autoPreamp(eq.filters).toFixed(1);
  if (rerender) renderEQ(); else drawPlot();
  if ($("#eq-live").checked) {
    clearTimeout(liveTimer);
    liveTimer = setTimeout(() => applyEQ(false), 250);
  }
}

async function applyEQ(persist) {
  if (eq.filters.some(f => !validFilter(f))) { if (persist) toast("Fix invalid filter values first", true); return; }
  try {
    const r = await api("eq", { eq, persist });
    if (persist) { savedEQ = clone(r.eq); toast("EQ saved"); }
    $("#eq-unsaved").hidden = persist || JSON.stringify(eq) === JSON.stringify(savedEQ);
  } catch (e) { toast(e.message, true); }
}

function loadPreset(id) {
  const p = presets.find(p => p.id === id);
  if (!p) return;
  eq.filters = clone(p.filters);
  eq.preset = id;
  if (id !== "flat") eq.enabled = true;
  renderEQ();
  if ($("#eq-live").checked) applyEQ(false);
}

// ---------------------------------------------------------------- forms
function fillForm(form, data) {
  for (const el of form.elements) {
    if (!el.name || !(el.name in data)) continue;
    if (el.type === "checkbox") el.checked = !!data[el.name];
    else el.value = data[el.name];
  }
}
function formData(form) {
  const o = {};
  for (const el of form.elements) {
    if (!el.name) continue;
    if (el.type === "checkbox") o[el.name] = el.checked;
    else if (el.type === "range" || el.type === "number" || el.name === "bitrate") o[el.name] = Number(el.value);
    else o[el.name] = el.value;
  }
  return o;
}

async function loadCards(current) {
  const sel = $("#output-device");
  sel.textContent = "";
  let cards = [];
  try { cards = await api("cards"); } catch (_) { /* ignore */ }
  for (const c of cards) sel.add(new Option(`${c.name} (${c.id})`, c.device));
  if (![...sel.options].some(o => o.value === current)) sel.add(new Option(current + (cards.length ? " (not detected)" : ""), current));
  sel.value = current;
}

// ---------------------------------------------------------------- status
async function refreshStatus() {
  try {
    const s = await api("status");
    const pill = (name, ok) => `<span class="pill ${ok ? "ok" : "bad"}">${name}</span>`;
    $("#svc").innerHTML = pill("librespot", s.librespot) + pill("DSP", s.camilladsp);
    const np = s.now_playing || {};
    const state = np.state || "idle";
    $("#np-state").textContent = { playing: "▶ Playing", paused: "❚❚ Paused", stopped: "■ Stopped", idle: "Idle — select this device in the Spotify app" }[state] || state;
    $("#np-title").textContent = np.name || "Nothing playing";
    $("#np-artist").textContent = [np.artists && np.artists.filter(Boolean).join(", "), np.album || np.show].filter(Boolean).join(" — ");
    let extra = [];
    if (np.duration_ms) {
      let pos = np.position_ms || 0;
      if (state === "playing" && np.position_at) pos += Date.now() - np.position_at;
      extra.push(`${fmtTime(Math.min(pos, np.duration_ms))} / ${fmtTime(np.duration_ms)}`);
    }
    if (np.client) extra.push("from " + np.client);
    if (np.volume !== undefined) extra.push("volume " + np.volume + "%");
    $("#np-extra").textContent = extra.join(" · ");
    const img = $("#np-cover");
    if (np.cover && /^https:\/\/i\.scdn\.co\//.test(np.cover)) { if (img.src !== np.cover) img.src = np.cover; img.hidden = false; }
    else img.hidden = true;
    const up = s.system_uptime_s, d = Math.floor(up / 86400), h = Math.floor(up % 86400 / 3600), m = Math.floor(up % 3600 / 60);
    $("#sysinfo").textContent = `${s.hostname} · ${s.ip.join(", ") || "no IP"} · up ${d ? d + "d " : ""}${h}h ${m}m` +
      (s.cpu_temp_c ? ` · ${s.cpu_temp_c.toFixed(1)} °C` : "") + ` · v${s.version}` + (s.settings_mounted ? "" : " · ⚠ settings partition not mounted");
    $("#eq-unsaved").hidden = !s.eq_unsaved;
  } catch (e) { $("#svc").innerHTML = '<span class="pill bad">offline</span>'; }
}

// ---------------------------------------------------------------- DAC board
async function loadBoards() {
  const { boards, current } = await api("board");
  const sel = $("#board");
  sel.innerHTML = "";
  if (!current) sel.add(new Option("(unknown – choose one)", ""));
  for (const b of boards) sel.add(new Option(b.label, b.id));
  sel.value = current;
}
$("#board-form").addEventListener("submit", async e => {
  e.preventDefault();
  const id = $("#board").value;
  if (!id) return toast("Choose a board", true);
  try {
    const r = await api("board", { id });
    $("#board-state").textContent = "· reboot required";
    toast(r.status);
  } catch (err) { toast(err.message, true); }
});

// ---------------------------------------------------------------- logs
async function loadLogs() {
  try {
    const out = $("#log-out");
    out.textContent = (await api("logs?service=" + $("#log-service").value)) || "(empty)";
    out.scrollTop = out.scrollHeight;
  } catch (e) { toast(e.message); }
}
$("#log-refresh").addEventListener("click", loadLogs);
$("#log-service").addEventListener("change", loadLogs);
$("#logs").addEventListener("toggle", e => { if (e.target.open) loadLogs(); });

// ---------------------------------------------------------------- init
async function init() {
  loadBoards().catch(() => {});
  const [cfg, ps] = await Promise.all([api("config"), api("presets")]);
  presets = ps;
  const sel = $("#eq-preset");
  presets.forEach(p => sel.add(new Option(p.name, p.id)));
  sel.add(new Option("Custom", "custom"));

  const sf = $("#spotify-form");
  fillForm(sf, cfg);
  const vol = sf.elements.initial_volume, vout = $("#vol-out");
  const showVol = () => (vout.textContent = vol.value + "%");
  vol.oninput = showVol; showVol();
  sf.onsubmit = async ev => {
    ev.preventDefault();
    try { toast((await api("spotify", formData(sf))).status); } catch (e) { toast(e.message, true); }
  };

  const of = $("#output-form");
  await loadCards(cfg.output_device);
  fillForm(of, { output_format: cfg.output_format });
  of.onsubmit = async ev => {
    ev.preventDefault();
    try { toast((await api("output", formData(of))).status); } catch (e) { toast(e.message, true); }
  };

  const pf = $("#password-form");
  $("#pw-state").textContent = cfg.has_password ? "(set)" : "(none)";
  pf.onsubmit = async ev => {
    ev.preventDefault();
    const pw = pf.elements.password.value;
    if (!pw && !confirm("Remove the password?")) return;
    try {
      toast((await api("password", { password: pw })).status);
      $("#pw-state").textContent = pw ? "(set)" : "(none)";
      pf.reset();
    } catch (e) { toast(e.message, true); }
  };

  savedEQ = clone(cfg.eq);
  eq = clone(cfg.live_eq || cfg.eq);
  eq.filters = eq.filters || [];

  $("#eq-enabled").onchange = e => { eq.enabled = e.target.checked; drawPlot(); applyEQ(false); };
  $("#eq-autopreamp").onchange = e => { eq.auto_preamp = e.target.checked; if (!eq.auto_preamp) eq.preamp = autoPreamp(eq.filters); changed(true); };
  $("#eq-preamp").oninput = e => { eq.preamp = +e.target.value; changed(); };
  sel.onchange = () => { if (sel.value !== "custom") loadPreset(sel.value); };
  $("#eq-add").onclick = () => { eq.filters.push({ enabled: true, type: "PK", freq: 1000, gain: 0, q: 1 }); changed(true); };
  $("#eq-clear").onclick = () => { eq.filters = []; changed(true); };
  $("#eq-save").onclick = () => applyEQ(true);
  $("#eq-revert").onclick = () => { eq = clone(savedEQ); renderEQ(); applyEQ(false); };

  $("#peq-file").onchange = async e => { const f = e.target.files[0]; if (f) $("#peq-text").value = await f.text(); };
  $("#peq-import").onclick = async () => {
    try {
      const r = await api("eq/parse", { text: $("#peq-text").value });
      eq.filters = r.filters;
      if (r.preamp !== null) { eq.preamp = r.preamp; eq.auto_preamp = false; }
      eq.enabled = true;
      eq.preset = "custom";
      renderEQ();
      if ($("#eq-live").checked) applyEQ(false);
      toast(`Imported ${r.filters.length} filters` + (r.warnings.length ? ` (${r.warnings[0]})` : "") + " — press Save EQ to keep");
    } catch (e) { toast(e.message, true); }
  };

  $("#reboot").onclick = async () => {
    if (!confirm("Reboot the device?")) return;
    try { await api("reboot", {}); toast("Rebooting… reload this page in ~30 s"); } catch (e) { toast(e.message, true); }
  };

  window.addEventListener("resize", drawPlot);
  renderEQ();
  refreshStatus();
  setInterval(refreshStatus, 2000);
}

init().catch(e => toast("Failed to load: " + e.message, true));
