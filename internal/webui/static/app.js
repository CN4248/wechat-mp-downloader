"use strict";

const $ = (sel, root = document) => root.querySelector(sel);
const KEY = "wxmp.settings.v1";
const JOB_KEY = "wxmp.job.v1";
const THEME_KEY = "wxmp.theme.v1";
const THEME_DARK = new Set(["dark", "midnight"]);
const THEME_LABEL = {
  auto: "跟随系统", sun: "阳光", mint: "薄荷", ocean: "海盐",
  sakura: "樱花", dark: "深色", midnight: "午夜蓝",
};
const THEME_BG = {
  sun: "#fbf5ea", mint: "#eef4f0", ocean: "#edf3fb",
  sakura: "#fdf1f5", dark: "#0d1411", midnight: "#0a0f1e",
};

const LIVE = new Set(["queued", "running", "paused"]);
const STATUS_LABEL = {
  queued: "排队中", running: "进行中", paused: "已暂停",
  done: "完成", failed: "失败", canceled: "已取消",
};

const state = {
  cfg: null,
  tokens: [],
  articles: [],
  collections: [],
  jobId: null,
  timer: null,
  running: false,
  logCount: 0,
  resultCount: 0,
  nameInvalid: false,
  previewTimer: null,
  overrides: {},
  subs: null,
  subResolved: null,
  subSel: new Set(),
  sincePick: null,
  tasks: [],
  taskSel: new Set(),
  queue: {},
  tasksTimer: null,
  subsTimer: null,
};

/* ---------- tiny helpers ---------- */

const esc = (s) =>
  String(s == null ? "" : s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));

function toast(msg, isErr) {
  const el = $("#toast");
  el.textContent = msg;
  el.className = "toast show" + (isErr ? " err" : "");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => { el.className = "toast"; }, 3200);
}

function showError(el, msg) {
  if (!msg) { el.classList.add("hidden"); el.textContent = ""; return; }
  el.textContent = msg;
  el.classList.remove("hidden");
}

function hide(el) { el.classList.add("hidden"); }

function setLoading(btn, on) {
  btn.disabled = on;
  btn.classList.toggle("loading", on);
}

function formatSize(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  const units = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
  return n.toFixed(n >= 10 ? 0 : 1) + " " + units[i];
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  let data = null;
  try { data = await res.json(); } catch (_) { /* ignore */ }
  if (!res.ok) {
    const msg = (data && (data.error || data.message)) || `请求失败（HTTP ${res.status}）`;
    throw new Error(msg);
  }
  return data;
}

/* ---------- settings ---------- */

function loadSettings() {
  try { return JSON.parse(localStorage.getItem(KEY) || "{}"); } catch (_) { return {}; }
}

function saveSettings() {
  try {
    localStorage.setItem(KEY, JSON.stringify({
      outDir: $("#outDir").value,
      nameTpl: $("#nameTpl").value,
      collectionTpl: $("#collectionTpl").value,
      jobs: $("#jobs").value,
      cover: $("#optCover").checked,
      video: $("#optVideo").checked,
      force: $("#optForce").checked,
    }));
  } catch (_) { /* storage may be disabled */ }
}

/* ---------- theme ---------- */

function resolveTheme(choice) {
  if (choice !== "auto") return choice;
  const dark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
  return dark ? "dark" : "mint";
}

function applyTheme(choice, persist) {
  const resolved = resolveTheme(choice);
  document.documentElement.setAttribute("data-theme", resolved);
  document.documentElement.setAttribute("data-theme-choice", choice);
  if (persist) {
    try { localStorage.setItem(THEME_KEY, choice); } catch (_) {}
  }
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", THEME_BG[resolved] || "#eef4f0");
  document.querySelectorAll("#themeSwitch .tsw").forEach((b) => {
    b.setAttribute("aria-pressed", String(b.dataset.themeSet === choice));
  });
}

function initTheme() {
  let choice = "auto";
  try { choice = localStorage.getItem(THEME_KEY) || "auto"; } catch (_) {}
  applyTheme(choice, false);
  document.querySelectorAll("#themeSwitch .tsw").forEach((b) => {
    b.addEventListener("click", () => {
      const next = b.dataset.themeSet;
      applyTheme(next, true);
      toast("已切换到「" + (THEME_LABEL[next] || next) + "」主题");
    });
  });
  if (window.matchMedia) {
    try {
      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
        let cur = "auto";
        try { cur = localStorage.getItem(THEME_KEY) || "auto"; } catch (_) {}
        if (cur === "auto") applyTheme("auto", false);
      });
    } catch (_) {}
  }
}

/* ---------- init ---------- */

function saveJob(id) {
  try { localStorage.setItem(JOB_KEY, JSON.stringify({ id, at: Date.now() })); } catch (_) {}
}

function loadJob() {
  try { return JSON.parse(localStorage.getItem(JOB_KEY) || "{}"); } catch (_) { return {}; }
}

async function restoreJob() {
  const saved = loadJob();
  if (!saved.id) return;
  try {
    const job = await api("/api/jobs/" + encodeURIComponent(saved.id));
    state.jobId = saved.id;
    state.running = LIVE.has(job.status);
    state.logCount = 0;
    state.resultCount = 0;
    $("#logs").textContent = "";
    $("#results").innerHTML = '<div class="empty">等待结果…</div>';
    $("#cardProgress").classList.remove("hidden");
    updateJobButtons(job);
    renderJob(job);
    if (state.running) {
      $("#btnDownload").classList.add("loading");
      $("#btnDownload").disabled = true;
      updateDownloadState();
      pollJob();
    } else {
      finishJob(job, true);
    }
  } catch (_) {
    // 服务重启或任务已被清理，丢掉过期的任务指针。
    try { localStorage.removeItem(JOB_KEY); } catch (_) {}
  }
}

const HISTORY_LABEL = STATUS_LABEL;

function statusClass(st) {
  if (st === "done") return "done";
  if (st === "failed") return "failed";
  if (st === "canceled") return "canceled";
  if (st === "paused") return "canceled";
  return "running";
}

function fmtRel(epoch) {
  if (!epoch || epoch < 0) return "待首次扫描";
  const diff = epoch - Math.floor(Date.now() / 1000);
  const abs = Math.abs(diff);
  const word = diff >= 0 ? "后" : "前";
  if (abs < 60) return abs + " 秒" + word;
  if (abs < 3600) return Math.round(abs / 60) + " 分钟" + word;
  if (abs < 86400) return Math.round(abs / 3600) + " 小时" + word;
  return Math.round(abs / 86400) + " 天" + word;
}

function historyTime(ts) {
  if (!ts) return "";
  const d = new Date(ts * 1000);
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function taskRowsHTML() {
  if (!state.tasks.length) {
    return '<div class="empty">暂无任务</div>';
  }
  return state.tasks.map((j) => {
    const checked = state.taskSel.has(j.id) ? " checked" : "";
    const name = j.source_name || (j.urls && j.urls[0]) || j.out_dir || "";
    const meta = `共 ${j.total || 0} 项 · 成功 ${j.success || 0} · 跳过 ${j.skipped || 0} · 失败 ${j.failed || 0}`;
    const live = LIVE.has(j.status);
    const acts = [];
    acts.push(`<button class="btn tiny ghost" data-act="view" data-id="${esc(j.id)}" type="button">查看</button>`);
    if (j.status === "queued" || j.status === "running") {
      acts.push(`<button class="btn tiny ghost" data-act="pause" data-id="${esc(j.id)}" type="button">暂停</button>`);
    }
    if (j.status === "paused") {
      acts.push(`<button class="btn tiny ghost" data-act="resume" data-id="${esc(j.id)}" type="button">开始</button>`);
    }
    if (live) {
      acts.push(`<button class="btn tiny ghost" data-act="cancel" data-id="${esc(j.id)}" type="button">取消</button>`);
    }
    if (!live) {
      acts.push(`<button class="btn tiny ghost" data-act="retry" data-id="${esc(j.id)}" type="button">重试</button>`);
    }
    acts.push(`<button class="btn tiny danger" data-act="delete" data-id="${esc(j.id)}" type="button">删除</button>`);
    return `<div class="history-row task-row" data-id="${esc(j.id)}">
      <input type="checkbox" data-task="${esc(j.id)}"${checked} aria-label="选择任务">
      <span class="history-status ${statusClass(j.status)}">${esc(HISTORY_LABEL[j.status] || j.status)}</span>
      <span class="history-main">
        <span class="history-url">${esc(name)}${j.source === "sub" ? '<span class="badge">追更</span>' : ""}</span>
        <span class="history-meta">${esc(meta)}${j.error ? " · " + esc(j.error) : ""}</span>
      </span>
      <span class="history-time">${esc(historyTime(j.started_at || j.finished_at))}</span>
      <span class="task-acts">${acts.join("")}</span>
    </div>`;
  }).join("");
}

function renderTasks(queue) {
  if (queue) state.queue = queue;
  const list = $("#historyList");
  if (!list) return;
  list.innerHTML = taskRowsHTML();
  const q = state.queue || {};
  const bits = [];
  if (q.paused_all) bits.push("已暂停全部");
  if (q.running) bits.push("运行中 " + q.running);
  if (q.queued) bits.push("排队 " + q.queued);
  if (q.paused) bits.push("暂停 " + q.paused);
  $("#queueMeta").textContent = bits.length ? "任务队列：" + bits.join(" · ") : "任务队列：空闲（记录保存在 data 卷）";
  $("#taskSelInfo").textContent = state.taskSel.size ? `已选 ${state.taskSel.size} 个` : "未选择";
  $("#taskSelectAll").checked = state.tasks.length > 0 && state.taskSel.size === state.tasks.length;
}

async function loadTasks(silent) {
  try {
    const data = await api("/api/jobs");
    state.tasks = data.jobs || [];
    const ids = new Set(state.tasks.map((j) => j.id));
    [...state.taskSel].forEach((id) => { if (!ids.has(id)) state.taskSel.delete(id); });
    renderTasks(data.queue || {});
  } catch (e) {
    if (!silent) toast("读取任务失败：" + e.message, true);
  }
}

const TASK_ACT_LABEL = { pause: "暂停", resume: "开始", cancel: "取消", retry: "重试", delete: "删除" };

async function taskAction(action, ids) {
  if (!ids.length) { toast("请先勾选任务", true); return; }
  try {
    const data = await api("/api/jobs/batch", {
      method: "POST",
      body: JSON.stringify({ action, ids }),
    });
    toast(`已${TASK_ACT_LABEL[action] || action} ${data.count || 0} 个任务`);
    state.taskSel.clear();
    await loadTasks(true);
    pollJob();
  } catch (e) {
    toast(e.message, true);
  }
}

async function taskGlobal(which) {
  const map = {
    pause: ["/api/jobs/pause-all", "已暂停全部任务"],
    resume: ["/api/jobs/resume-all", "已开始全部任务"],
    retry: ["/api/jobs/retry-failed", null],
    clear: ["/api/jobs/clear-completed", null],
  };
  const [path, msg] = map[which];
  try {
    const data = await api(path, { method: "POST" });
    if (which === "retry") toast(`已重新排队 ${data.count || 0} 个失败任务`);
    else if (which === "clear") toast(`已删除 ${data.deleted || 0} 个已完成任务`);
    else toast(msg);
    await loadTasks(true);
    pollJob();
  } catch (e) {
    toast(e.message, true);
  }
}

async function openJob(id) {
  if (!id) return;
  try {
    const job = await api("/api/jobs/" + encodeURIComponent(id));
    if (state.timer) { clearTimeout(state.timer); state.timer = null; }
    state.jobId = id;
    state.running = LIVE.has(job.status);
    state.logCount = 0;
    state.resultCount = 0;
    saveJob(id);
    $("#logs").textContent = "";
    $("#results").innerHTML = '<div class="empty">等待结果…</div>';
    $("#cardProgress").classList.remove("hidden");
    updateJobButtons(job);
    renderJob(job);
    if (state.running) {
      $("#btnDownload").classList.add("loading");
      $("#btnDownload").disabled = true;
      pollJob();
    } else {
      finishJob(job, true);
    }
    $("#cardProgress").scrollIntoView({ behavior: "smooth", block: "start" });
  } catch (e) {
    toast("读取历史任务失败：" + e.message, true);
  }
}

async function init() {
  initTheme();
  bindEvents();
  const saved = loadSettings();
  const outDir = $("#outDir");
  const nameTpl = $("#nameTpl");
  const collectionTpl = $("#collectionTpl");
  const jobs = $("#jobs");

  try {
    const cfg = await api("/api/config");
    state.cfg = cfg;
    state.tokens = cfg.tokens || [];
    outDir.value = saved.outDir || cfg.default_out || "";
    nameTpl.value = saved.nameTpl || cfg.default_name || "<title>-<post_id>-<order>.<ext>";
    collectionTpl.value = saved.collectionTpl || cfg.default_collection || "<collection_title>/<collection_order>-<title>/<post_id>-<order>.<ext>";
    jobs.value = saved.jobs || cfg.jobs || 4;
    $("#optCover").checked = !!saved.cover;
    $("#optVideo").checked = !!saved.video;
    $("#optForce").checked = !!saved.force;
    const dataList = $("#dirOptions");
    if (dataList) {
      dataList.innerHTML = (cfg.download_roots || []).map((d) => `<option value="${esc(d)}"></option>`).join("");
    }
    $("#footVer").textContent = cfg.app + " v" + cfg.version;
    renderTokens();
    const pill = $("#serverState");
    pill.classList.remove("bad");
    pill.querySelector("span").textContent = "服务正常";
  } catch (e) {
    const pill = $("#serverState");
    pill.classList.add("bad");
    pill.querySelector("span").textContent = "服务异常";
    toast("无法读取服务配置：" + e.message, true);
    outDir.value = saved.outDir || "";
    nameTpl.value = saved.nameTpl || "<title>-<post_id>-<order>.<ext>";
    collectionTpl.value = saved.collectionTpl || "<collection_title>/<collection_order>-<title>/<post_id>-<order>.<ext>";
    jobs.value = saved.jobs || 4;
  }
  updateHint();
  await restoreJob();
  await loadTasks(true);
  await loadSubs(true);
  setInterval(checkHealth, 60000);
  setInterval(() => { if (!document.hidden) loadTasks(true); }, 12000);
  setInterval(() => { if (!document.hidden) loadSubs(true); }, 20000);

  // 支持 ?url=<文章链接> 直接解析，方便分享与自测。
  const pre = new URLSearchParams(location.search).get("url");
  if (pre) {
    $("#urls").value = pre;
    doParse();
  }
}

async function checkHealth() {
  try {
    await api("/api/health");
    $("#serverState").classList.remove("bad");
    $("#serverState span").textContent = "服务正常";
  } catch (_) {
    $("#serverState").classList.add("bad");
    $("#serverState span").textContent = "服务异常";
  }
}

function renderTokens() {
  const box = $("#tokens");
  box.innerHTML = "";
  state.tokens.forEach((t) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "chip";
    b.textContent = t.token;
    b.title = `${t.desc || ""}${t.alias ? "（" + t.alias + "）" : ""}${t.example ? "，示例：" + t.example : ""}`;
    b.addEventListener("click", () => insertToken(t.token));
    box.appendChild(b);
  });
}

function insertToken(token) {
  const input = $("#nameTpl");
  const start = input.selectionStart ?? input.value.length;
  const end = input.selectionEnd ?? input.value.length;
  input.value = input.value.slice(0, start) + token + input.value.slice(end);
  const pos = start + token.length;
  input.setSelectionRange(pos, pos);
  input.focus();
  saveSettings();
  schedulePreview();
}

/* ---------- events ---------- */

function bindEvents() {
  $("#btnParse").addEventListener("click", doParse);
  $("#btnClear").addEventListener("click", () => {
    $("#urls").value = "";
    $("#urls").focus();
  });
  $("#urls").addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") doParse();
  });

  $("#btnDownload").addEventListener("click", doDownload);
  $("#btnCancel").addEventListener("click", doCancel);
  $("#btnPause").addEventListener("click", doPause);
  $("#btnResume").addEventListener("click", doResume);
  $("#btnOpenDir").addEventListener("click", doOpenDir);
  $("#btnResetName").addEventListener("click", () => {
    $("#nameTpl").value = (state.cfg && state.cfg.default_name) || "<title>-<post_id>-<order>.<ext>";
    saveSettings();
    schedulePreview();
  });
  $("#btnResetCollection").addEventListener("click", () => {
    $("#collectionTpl").value = (state.cfg && state.cfg.default_collection) || "<collection_title>/<collection_order>-<title>/<post_id>-<order>.<ext>";
    saveSettings();
  });
  $("#btnCollapsePreview").addEventListener("click", () => {
    const hidden = !$("#articles").classList.contains("hidden");
    $("#articles").classList.toggle("hidden", hidden);
    $("#collections").classList.toggle("hidden", hidden);
    $("#btnCollapsePreview").textContent = hidden ? "展开" : "收起";
  });

  ["#outDir", "#jobs", "#nameTpl", "#collectionTpl", "#optCover", "#optVideo", "#optForce"].forEach((sel) => {
    const el = $(sel);
    el.addEventListener("input", saveSettings);
    el.addEventListener("change", saveSettings);
  });
  $("#nameTpl").addEventListener("input", schedulePreview);

  $("#articles").addEventListener("click", (e) => {
    const fig = e.target.closest(".thumb");
    if (!fig) return;
    const a = state.articles[Number(fig.dataset.a)];
    const m = a && a.medias && a.medias[Number(fig.dataset.m)];
    if (!m) return;
    openLightbox(m);
  });
  $("#articles").addEventListener("error", (e) => {
    const img = e.target;
    if (img && img.tagName === "IMG") img.remove();
  }, true);

  ["#articles", "#collections"].forEach((sel) => {
    const box = $(sel);
    box.addEventListener("input", (e) => {
      const input = e.target.closest("[data-ov]");
      if (!input) return;
      const key = input.dataset.ov;
      const field = input.dataset.ovf;
      state.overrides[key] = state.overrides[key] || {};
      state.overrides[key][field] = input.value;
      syncOverridePlaceholders();
    });
    box.addEventListener("click", (e) => {
      const btn = e.target.closest("[data-ovreset]");
      if (!btn) return;
      const key = btn.dataset.ovreset;
      delete state.overrides[key];
      box.querySelectorAll(`[data-ov="${key}"]`).forEach((el) => { el.value = ""; });
      syncOverridePlaceholders();
    });
  });

  $("#btnTasksReload").addEventListener("click", () => loadTasks());
  $("#btnPauseAll").addEventListener("click", () => taskGlobal("pause"));
  $("#btnResumeAll").addEventListener("click", () => taskGlobal("resume"));
  $("#btnRetryFailed").addEventListener("click", () => taskGlobal("retry"));
  $("#btnClearDone").addEventListener("click", () => taskGlobal("clear"));
  $("#btnTaskPause").addEventListener("click", () => taskAction("pause", [...state.taskSel]));
  $("#btnTaskResume").addEventListener("click", () => taskAction("resume", [...state.taskSel]));
  $("#btnTaskCancel").addEventListener("click", () => taskAction("cancel", [...state.taskSel]));
  $("#btnTaskRetry").addEventListener("click", () => taskAction("retry", [...state.taskSel]));
  $("#btnTaskDelete").addEventListener("click", () => taskAction("delete", [...state.taskSel]));
  $("#taskSelectAll").addEventListener("change", (e) => {
    state.taskSel.clear();
    if (e.target.checked) state.tasks.forEach((j) => state.taskSel.add(j.id));
    renderTasks();
  });
  $("#historyList").addEventListener("change", (e) => {
    const cb = e.target.closest("[data-task]");
    if (!cb) return;
    if (cb.checked) state.taskSel.add(cb.dataset.task);
    else state.taskSel.delete(cb.dataset.task);
    renderTasks();
  });
  $("#historyList").addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-act]");
    if (!btn) return;
    const id = btn.dataset.id;
    const act = btn.dataset.act;
    if (act === "view") { openJob(id); return; }
    if (act === "delete" && !confirm("删除这条任务记录？已下载的文件不会被删除。")) return;
    await taskAction(act, [id]);
  });

  $("#btnSubConfig").addEventListener("click", saveSubConfig);
  $("#btnSaveCookie").addEventListener("click", () => saveCookie(false));
  $("#btnClearCookie").addEventListener("click", () => saveCookie(true));
  $("#btnSubResolve").addEventListener("click", doSubResolve);
  $("#btnSubAdd").addEventListener("click", doSubAdd);
  $("#btnSubSearch").addEventListener("click", doSubSearch);
  $("#subQuery").addEventListener("keydown", (e) => { if (e.key === "Enter") doSubSearch(); });
  $("#subUrl").addEventListener("keydown", (e) => { if (e.key === "Enter") doSubResolve(); });
  $("#subSelectAll").addEventListener("change", (e) => {
    state.subSel.clear();
    if (e.target.checked) (state.subs ? state.subs.subs : []).forEach((x) => state.subSel.add(x.id));
    renderSubs();
  });
  $("#btnSubEnable").addEventListener("click", () => subBatch("enable"));
  $("#btnSubDisable").addEventListener("click", () => subBatch("disable"));
  $("#btnSubCheck").addEventListener("click", () => subBatch("check"));
  $("#btnSubDelete").addEventListener("click", () => subBatch("delete"));
  $("#subList").addEventListener("click", (e) => {
    const btn = e.target.closest("[data-subact]");
    if (!btn) return;
    const row = btn.closest("[data-id]");
    if (row) subAct(row.dataset.id, btn.dataset.subact, row);
  });
  $("#subList").addEventListener("change", (e) => {
    const cb = e.target.closest("[data-subsel]");
    if (!cb) return;
    if (cb.checked) state.subSel.add(cb.dataset.subsel);
    else state.subSel.delete(cb.dataset.subsel);
    $("#subSelInfo").textContent = state.subSel.size ? `已选 ${state.subSel.size} 个` : "未选择";
  });

  $("#lbClose").addEventListener("click", closeLightbox);
  $("#lightbox").addEventListener("click", (e) => {
    if (e.target === $("#lightbox")) closeLightbox();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeLightbox();
  });
}

/* ---------- parse ---------- */

async function doParse() {
  const raw = $("#urls").value.trim();
  if (!raw) {
    toast("请先粘贴文章链接", true);
    $("#urls").focus();
    return;
  }
  hide($("#parseError"));
  const btn = $("#btnParse");
  setLoading(btn, true);
  try {
    const data = await api("/api/parse", {
      method: "POST",
      body: JSON.stringify({
        urls: [raw],
        name_template: $("#nameTpl").value.trim(),
        collection_template: $("#collectionTpl").value.trim(),
        cover: $("#optCover").checked,
        video: $("#optVideo").checked,
      }),
    });
    state.articles = data.articles || [];
    state.collections = data.collections || [];
    renderArticles();
    renderCollections();
    $("#cardPreview").classList.remove("hidden");
    updateSummary();
    updateDownloadState();
    schedulePreview();
    const ok = state.articles.filter((a) => a.medias && a.medias.length);
    const cols = state.collections.filter((c) => c.articles && c.articles.length);
    if (!ok.length && !cols.length) {
      const msg = state.articles.concat(state.collections).map((a) => a.error).filter(Boolean).join("\n") || "没有解析到图片";
      showError($("#parseError"), msg);
      toast("没有解析到可下载的图片", true);
    } else {
      if (state.articles.some((a) => a.error)) {
        showError($("#parseError"), state.articles.map((a) => a.error).filter(Boolean).join("\n"));
      }
      $("#cardPreview").scrollIntoView({ behavior: "smooth", block: "start" });
      const imgs = ok.reduce((n, a) => n + a.medias.length, 0);
      const arts = cols.reduce((n, c) => n + c.articles.length, 0);
      toast("解析完成" + (imgs ? "，图片 " + imgs + " 项" : "") + (arts ? "，合集 " + arts + " 篇" : ""));
    }
  } catch (e) {
    showError($("#parseError"), e.message);
    toast(e.message, true);
  } finally {
    setLoading(btn, false);
  }
}

function renderArticles() {
  const box = $("#articles");
  box.innerHTML = state.articles.map((a, ai) => articleHTML(a, ai)).join("");
  box.classList.remove("hidden");
  $("#btnCollapsePreview").textContent = "收起";
}

function renderCollections() {
  const box = $("#collections");
  if (!box) return;
  box.innerHTML = state.collections.map((c, ci) => collectionHTML(c, ci)).join("");
  box.classList.toggle("hidden", !state.collections.length);
}

function collectionHTML(c, ci) {
  const articles = c.articles || [];
  const rows = articles.map((a, ai) => `<li data-ci="${ci}" data-ai="${ai}">
    <span class="col-order">${a.order || ai + 1}</span>
    <span class="col-title">${esc(a.title || "未命名")}</span>
    <code>${esc(a.url)}</code></li>`).join("");
  const ovc = state.overrides["c" + ci] || {};
  const ovRow = `<div class="ov-row">
    <span class="ov-title">本次任务临时设置（留空＝用全局设置）</span>
    <label><span>下载路径</span><input data-ov="c${ci}" data-ovf="dir" value="${esc(ovc.dir || "")}" placeholder="留空＝${esc($("#outDir").value || "全局目录")}" list="dirOptions" spellcheck="false"></label>
    <label><span>合集规则</span><input data-ov="c${ci}" data-ovf="tpl" value="${esc(ovc.tpl || "")}" placeholder="留空＝${esc($("#collectionTpl").value || "全局合集规则")}" spellcheck="false"></label>
    <button class="btn tiny ghost" type="button" data-ovreset="c${ci}">恢复全局</button>
  </div>`;
  return `<article class="article collection" data-collection="${ci}">
    <div class="article-head">
      <div>
        <div class="article-title">📚 ${esc(c.title || "未命名合集")}</div>
        <div class="article-meta">${esc(c.account || "")} · id ${esc(c.id)} · ${c.article_count || articles.length} 篇</div>
      </div>
      <div class="article-counts"><span class="tag">合集 ${articles.length}</span></div>
    </div>
    ${ovRow}
    ${c.error ? `<div class="alert">${esc(c.error)}</div>` : ""}
    ${(c.warnings || []).length ? `<div class="alert warn">${c.warnings.map(esc).join("；")}</div>` : ""}
    <div class="collection-hint">下载时按合集命名规则逐个文章解析，图片会保存到对应子目录。</div>
    <ul class="collection-list">${rows}</ul>
  </article>`;
}

function articleHTML(a, ai) {
  const failed = !!a.error && (!a.medias || !a.medias.length);
  const medias = a.medias || [];
  const tags = [];
  if (a.image_count) tags.push(`<span class="tag">图片 ${a.image_count}</span>`);
  if (a.gif_count) tags.push(`<span class="tag gif">动图 ${a.gif_count}</span>`);
  if (a.video_count) tags.push(`<span class="tag video">视频 ${a.video_count}</span>`);

  const thumbs = medias.map((m, mi) => {
    const src = "/api/thumb?u=" + encodeURIComponent(m.preview_url || m.url) + "&s=300";
    return `<figure class="thumb" data-a="${ai}" data-m="${mi}" title="${esc(m.rel_path)}">
      <div class="ph"><img loading="lazy" src="${src}" alt="${esc(m.name)}">
      ${m.is_gif ? '<span class="gif-badge">GIF</span>' : ""}</div>
      <figcaption>#${m.seq} · ${m.width}×${m.height} · ${esc((m.ext || "").toUpperCase())}</figcaption>
    </figure>`;
  }).join("");

  const meta = [
    a.account ? esc(a.account) : "",
    a.publish_text ? esc(a.publish_text) : "",
    a.post_id ? "id " + esc(a.post_id) : "",
    a.content_count ? `正文 ${a.content_count}` : "",
    a.picture_count ? `图集 ${a.picture_count}` : "",
    `${medias.length} 项`,
  ].filter(Boolean).join(" · ");

  const first = medias.find((m) => m.rel_path);
  const foot = first
    ? `<div class="article-foot"><span>首个文件</span><code data-first="${ai}">${esc(first.rel_path)}</code></div>`
    : "";
  const ov = state.overrides["a" + ai] || {};
  const ovRow = `<div class="ov-row">
    <span class="ov-title">本次任务临时设置（留空＝用全局设置）</span>
    <label><span>下载路径</span><input data-ov="a${ai}" data-ovf="dir" value="${esc(ov.dir || "")}" placeholder="留空＝${esc($("#outDir").value || "全局目录")}" list="dirOptions" spellcheck="false"></label>
    <label><span>重命名规则</span><input data-ov="a${ai}" data-ovf="tpl" value="${esc(ov.tpl || "")}" placeholder="留空＝${esc($("#nameTpl").value || "全局规则")}" spellcheck="false"></label>
    <button class="btn tiny ghost" type="button" data-ovreset="a${ai}">恢复全局</button>
  </div>`;

  return `<article class="article${failed ? " failed" : ""}" data-article="${ai}">
    <div class="article-head">
      <div>
        <div class="article-title">${esc(a.title || "未命名文章")}</div>
        <div class="article-meta">${meta}${a.author && a.author !== a.account ? " · " + esc(a.author) : ""}</div>
      </div>
      <div class="article-counts">${tags.join("")}</div>
    </div>
    ${ovRow}
    ${a.error ? `<div class="alert">${esc(a.error)}</div>` : ""}
    ${(a.warnings || []).length ? `<div class="alert warn">${a.warnings.map(esc).join("；")}</div>` : ""}
    ${thumbs ? `<div class="thumbs">${thumbs}</div>` : ""}
    ${foot}
  </article>`;
}

function updateSummary() {
  const total = state.articles.reduce((n, a) => n + (a.medias || []).length, 0);
  const gifs = state.articles.reduce((n, a) => n + (a.gif_count || 0), 0);
  const vids = state.articles.reduce((n, a) => n + (a.video_count || 0), 0);
  const arts = state.collections.reduce((n, c) => n + ((c.articles || []).length), 0);
  const parts = [];
  if (state.articles.length) parts.push(`${state.articles.length} 篇文章 · ${total} 项`);
  if (state.collections.length) parts.push(`${state.collections.length} 个合集 · ${arts} 篇`);
  $("#previewSummary").textContent = (parts.join(" · ") || "解析完成后在这里预览将下载的图片") +
    (gifs ? ` · 动图 ${gifs}` : "") + (vids ? ` · 视频 ${vids}` : "");
}

function updateDownloadState() {
  const total = state.articles.reduce((n, a) => n + (a.medias || []).length, 0);
  const arts = state.collections.reduce((n, c) => n + ((c.articles || []).length), 0);
  const btn = $("#btnDownload");
  btn.disabled = (total === 0 && arts === 0) || state.nameInvalid;
  const label = total && arts ? `开始下载（${total} 项 + 合集 ${arts} 篇）`
    : arts ? `开始下载（合集 ${arts} 篇）`
    : total ? `开始下载（${total} 项）` : "开始下载";
  btn.textContent = state.running ? "加入下载队列" : label;
  if (!state.running) {
    $("#downloadHint").textContent = total > 0
      ? `将下载 ${total} 项到 ${$("#outDir").value || "默认目录"}`
      : "解析文章后即可开始下载。";
  }
}

function updateHint() {
  $("#downloadHint").textContent = $("#outDir").value
    ? "下载目录：" + $("#outDir").value
    : "解析文章后即可开始下载。";
}

/* ---------- naming preview ---------- */

function schedulePreview() {
  clearTimeout(state.previewTimer);
  state.previewTimer = setTimeout(runPreview, 320);
}

function flattenMedias() {
  const out = [];
  state.articles.forEach((a, ai) => {
    (a.medias || []).forEach((m, mi) => {
      out.push({ ai, mi, m });
    });
  });
  return out;
}

function mediaPayload(m) {
  return {
    seq: m.seq, kind: m.kind, url: m.url, preview_url: m.preview_url,
    ext: m.ext, width: m.width, height: m.height, is_gif: m.is_gif,
    source: m.source, file_id: m.file_id, title: m.title, post_id: m.post_id,
    account: m.account, author: m.author, biz: m.biz, sn: m.sn,
    mid: m.mid, idx: m.idx, publish_time: m.publish_time, article_url: m.article_url,
  };
}

async function runPreview() {
  const flat = flattenMedias();
  const sample = $("#nameSample");
  const more = $("#nameMore");
  if (!flat.length) {
    sample.textContent = "—";
    more.textContent = "";
    state.nameInvalid = false;
    updateDownloadState();
    return;
  }
  try {
    const data = await api("/api/preview", {
      method: "POST",
      body: JSON.stringify({
        template: $("#nameTpl").value.trim(),
        medias: flat.map((f) => mediaPayload(f.m)),
      }),
    });
    state.nameInvalid = false;
    data.names.forEach((name, i) => {
      if (!name) return;
      const f = flat[i];
      f.m.rel_path = name;
      f.m.name = name.split("/").pop();
      const code = document.querySelector(`[data-first="${f.ai}"]`);
      if (code && f.mi === 0) code.textContent = name;
      const fig = document.querySelector(`.thumb[data-a="${f.ai}"][data-m="${f.mi}"]`);
      if (fig) fig.title = name;
    });
    sample.textContent = data.names.find(Boolean) || "—";
    more.textContent = `共 ${flat.length} 个文件`;
    hide($("#downloadError"));
  } catch (e) {
    state.nameInvalid = true;
    sample.textContent = "命名规则错误：" + e.message;
    more.textContent = "";
  }
  updateDownloadState();
}

/* ---------- download ---------- */

function buildTasks() {
  const tasks = [];
  state.articles.forEach((a, ai) => {
    if (!(a.medias && a.medias.length)) return;
    const ov = state.overrides["a" + ai] || {};
    tasks.push({
      url: a.final_url || a.url,
      out_dir: (ov.dir || "").trim(),
      name_template: (ov.tpl || "").trim(),
    });
  });
  state.collections.forEach((c, ci) => {
    if (!(c.articles || []).length) return;
    const ov = state.overrides["c" + ci] || {};
    tasks.push({
      url: c.final_url || c.url,
      out_dir: (ov.dir || "").trim(),
      collection_template: (ov.tpl || "").trim(),
    });
  });
  return tasks;
}

async function doDownload() {
  const tasks = buildTasks();
  if (!tasks.length) {
    toast("请先解析文章或合集", true);
    return;
  }
  hide($("#downloadError"));
  const btn = $("#btnDownload");
  setLoading(btn, true);
  try {
    const data = await api("/api/download", {
      method: "POST",
      body: JSON.stringify({
        tasks,
        out_dir: $("#outDir").value.trim(),
        jobs: Number($("#jobs").value) || 4,
        force: $("#optForce").checked,
        cover: $("#optCover").checked,
        video: $("#optVideo").checked,
        name_template: $("#nameTpl").value.trim(),
        collection_template: $("#collectionTpl").value.trim(),
      }),
    });
    state.jobId = data.job.id;
    saveJob(data.job.id);
    state.logCount = 0;
    state.resultCount = 0;
    state.running = true;
    $("#logs").textContent = "";
    $("#results").innerHTML = '<div class="empty">等待结果…</div>';
    $("#bar").style.width = "0%";
    $("#bar").className = "bar";
    $("#cardProgress").classList.remove("hidden");
    $("#btnCancel").classList.remove("hidden");
    $("#jobMeta").textContent = "任务 " + data.job.id + " · " + (data.job.out_dir || "");
    updateDownloadState();
    $("#cardProgress").scrollIntoView({ behavior: "smooth", block: "start" });
    pollJob();
  } catch (e) {
    showError($("#downloadError"), e.message);
    toast(e.message, true);
    setLoading(btn, false);
  }
}

async function pollJob() {
  if (!state.jobId) return;
  try {
    const job = await api("/api/jobs/" + encodeURIComponent(state.jobId));
    state.pollFails = 0;
    state.running = LIVE.has(job.status);
    renderJob(job);
    updateJobButtons(job);
    if (LIVE.has(job.status)) {
      state.timer = setTimeout(pollJob, job.status === "paused" ? 4000 : 800);
      return;
    }
    finishJob(job);
  } catch (e) {
    state.pollFails = (state.pollFails || 0) + 1;
    if (state.pollFails >= 4) {
      // 任务可能已被删除或服务重启：停止轮询，避免每 1.5 秒弹一次错误。
      toast("任务已不可用：" + e.message, true);
      $("#cardProgress").classList.add("hidden");
      state.running = false;
      state.jobId = null;
      try { localStorage.removeItem(JOB_KEY); } catch (_) {}
      updateDownloadState();
      return;
    }
    state.timer = setTimeout(pollJob, 1500);
  }
}

function renderJob(job) {
  const total = job.total || 0;
  const done = job.done || 0;
  const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : (LIVE.has(job.status) ? 3 : 0);
  $("#bar").style.width = pct + "%";
  $("#bar").className = "bar" + (job.status === "failed" ? " failed"
    : job.status === "canceled" || job.status === "paused" ? " canceled" : "");
  $("#statDone").textContent = done;
  $("#statOk").textContent = job.success || 0;
  $("#statSkip").textContent = job.skipped || 0;
  $("#statFail").textContent = job.failed || 0;
  $("#jobMeta").textContent =
    `任务 ${job.id} · ${job.out_dir} · 并发 ${job.jobs}` + (total ? ` · 共 ${total} 项` : "");

  const logs = $("#logs");
  if (job.logs && job.logs.length > state.logCount) {
    const nearBottom = logs.scrollTop + logs.clientHeight >= logs.scrollHeight - 30;
    const frag = document.createDocumentFragment();
    job.logs.slice(state.logCount).forEach((l) => {
      const div = document.createElement("div");
      div.textContent = l.time + "  " + l.text;
      if (l.text.includes("✗")) div.className = "err";
      else if (l.text.includes("✓")) div.className = "ok";
      frag.appendChild(div);
    });
    logs.appendChild(frag);
    state.logCount = job.logs.length;
    if (nearBottom) logs.scrollTop = logs.scrollHeight;
  }

  if (job.results && job.results.length !== state.resultCount) {
    state.resultCount = job.results.length;
    if (!job.results.length) {
      $("#results").innerHTML = '<div class="empty">等待结果…</div>';
    } else {
      $("#results").innerHTML = job.results.map(resultHTML).join("");
    }
  }
}

function resultHTML(r) {
  const ok = r.status === "ok";
  const skip = r.status === "skipped";
  const ico = ok ? "✓" : skip ? "=" : "✗";
  const cls = ok ? "ok" : skip ? "skip" : "fail";
  const title = r.error || r.warning || r.rel_path || "";
  return `<div class="result-row" title="${esc(title)}">
    <span class="ico ${cls}">${ico}</span>
    <span class="rname">${esc(r.rel_path || r.name || "")}</span>
    <span class="rsize">${ok ? formatSize(r.size) : skip ? "跳过" : "失败"}</span>
  </div>`;
}

function finishJob(job, silent) {
  state.running = false;
  loadTasks(true);
  $("#btnCancel").classList.add("hidden");
  $("#btnPause").classList.add("hidden");
  $("#btnResume").classList.add("hidden");
  $("#btnDownload").classList.remove("loading");
  $("#btnDownload").disabled = false;
  const map = { done: "完成", canceled: "已取消", failed: "失败", paused: "已暂停" };
  const label = map[job.status] || job.status;
  $("#jobMeta").textContent =
    `任务 ${job.id} · ${label} · 成功 ${job.success || 0}，跳过 ${job.skipped || 0}，失败 ${job.failed || 0}`;
  if (job.error) $("#jobMeta").textContent += " · " + job.error;
  const msg = `${label}：成功 ${job.success || 0}，跳过 ${job.skipped || 0}，失败 ${job.failed || 0}`;
  if (!silent) toast(msg, job.status === "failed");
  updateDownloadState();
  if (job.status === "done" || job.status === "canceled") {
    $("#bar").style.width = "100%";
  }
}

async function doCancel() {
  if (!state.jobId) return;
  try {
    await api("/api/jobs/" + encodeURIComponent(state.jobId) + "/cancel", { method: "POST" });
    toast("已请求取消，正在停止…");
    $("#btnCancel").disabled = true;
    setTimeout(() => { $("#btnCancel").disabled = false; }, 2000);
  } catch (e) {
    toast(e.message, true);
  }
}

async function doOpenDir() {
  const path = $("#outDir").value.trim();
  if (!path) { toast("请先填写下载目录", true); return; }
  try {
    const r = await api("/api/open-dir", { method: "POST", body: JSON.stringify({ path }) });
    if (r.ok) toast("已打开目录");
    else toast(r.message || "无法打开目录", true);
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- lightbox ---------- */

function openLightbox(m) {
  $("#lbImg").src = "/api/thumb?u=" + encodeURIComponent(m.preview_url || m.url) + "&s=640";
  $("#lbCap").textContent = `${m.name || ""} · ${m.width}×${m.height}` + (m.is_gif ? " · GIF" : "");
  $("#lightbox").classList.remove("hidden");
}

function closeLightbox() {
  $("#lightbox").classList.add("hidden");
  $("#lbImg").removeAttribute("src");
}


/* ---------- job pause / resume ---------- */

function syncOverridePlaceholders() {
  const dir = $("#outDir").value || "默认目录";
  const tpl = $("#nameTpl").value || "默认规则";
  const ctpl = $("#collectionTpl").value || "默认合集规则";
  document.querySelectorAll('[data-ov][data-ovf="dir"]').forEach((el) => {
    el.placeholder = "目录：" + dir;
  });
  document.querySelectorAll('[data-ov][data-ovf="tpl"]').forEach((el) => {
    const isCol = el.dataset.ov.startsWith("c");
    el.placeholder = "规则：" + (isCol ? ctpl : tpl);
  });
}

function updateJobButtons(job) {
  const st = job && job.status;
  $("#btnCancel").classList.toggle("hidden", !LIVE.has(st));
  $("#btnPause").classList.toggle("hidden", !(st === "running" || st === "queued"));
  $("#btnResume").classList.toggle("hidden", st !== "paused");
}

async function doPause() {
  if (!state.jobId) return;
  try {
    await api("/api/jobs/" + encodeURIComponent(state.jobId) + "/pause", { method: "POST" });
    toast("已请求暂停：当前文件下载完就停");
    await loadTasks(true);
    pollJob();
  } catch (e) { toast(e.message, true); }
}

async function doResume() {
  if (!state.jobId) return;
  try {
    await api("/api/jobs/" + encodeURIComponent(state.jobId) + "/resume", { method: "POST" });
    toast("已重新排队，已下载的文件会跳过");
    await loadTasks(true);
    pollJob();
  } catch (e) { toast(e.message, true); }
}

/* ---------- 定时追更 ---------- */

async function loadSubs(silent) {
  try {
    const data = await api("/api/subs");
    state.subs = data;
    const ids = new Set((data.subs || []).map((x) => x.id));
    [...state.subSel].forEach((id) => { if (!ids.has(id)) state.subSel.delete(id); });
    renderSubs();
  } catch (e) {
    if (!silent) toast("读取追更任务失败：" + e.message, true);
  }
}

function renderSubs() {
  const d = state.subs;
  if (!d) return;
  const cfg = d.config || {};
  $("#subEnabled").checked = !!cfg.enabled;
  if (document.activeElement !== $("#subInterval")) {
    $("#subInterval").value = Math.max(2, Math.round((cfg.interval || 600) / 60));
  }
  if (document.activeElement !== $("#subMaxNew")) $("#subMaxNew").value = cfg.max_new || 10;
  if (document.activeElement !== $("#subMinGap")) $("#subMinGap").value = cfg.min_gap || 60;

  const pill = $("#subState");
  const subs = d.subs || [];
  pill.classList.toggle("bad", !cfg.enabled);
  pill.querySelector("span").textContent = cfg.enabled
    ? (subs.length ? "追更中 · 下次 " + fmtRel(d.next_run) : "已启用，还没有追更任务")
    : "已关闭";
  $("#subNextRun").textContent = cfg.enabled && d.next_run > 0 ? "下次扫描：" + fmtRel(d.next_run) : "";
  renderCookieState(d.cookie || {});

  const list = $("#subList");
  if (document.querySelector(".since-picker:not(.hidden)")) {
    // 正在「选择起点」时不要被轮询刷新掉，列表等选完再重画。
    $("#subSelInfo").textContent = state.subSel.size ? `已选 ${state.subSel.size} 个` : "未选择";
    return;
  }
  if (!subs.length) {
    list.innerHTML = '<div class="empty">还没有追更任务，先在上面的输入框粘贴合集或文章链接。</div>';
  } else {
    list.innerHTML = subs.map(subHTML).join("");
  }
  $("#subSelInfo").textContent = state.subSel.size ? `已选 ${state.subSel.size} 个` : "未选择";
  $("#subSelectAll").checked = subs.length > 0 && state.subSel.size === subs.length;
}

function subHTML(s) {
  const checked = state.subSel.has(s.id) ? " checked" : "";
  const kind = s.kind === "album" ? "合集" : s.kind === "author" ? "全部文章" : "公众号";
  const albums = s.albums || [];
  const albumText = albums.length
    ? albums.map((a) => esc(a.title || a.id)).join("、")
    : "没有公开合集";
  const live = s.enabled ? "done" : "canceled";
  const msg = s.last_message || "还没扫描过";
  return `<div class="sub-row" data-id="${esc(s.id)}">
    <input type="checkbox" data-subsel="${esc(s.id)}"${checked} aria-label="选择追更任务">
    <div class="sub-main">
      <div class="sub-title">
        <span class="badge">${kind}</span>
        <b>${esc(s.name || s.target)}</b>
        ${s.account && s.account !== s.name ? `<span class="sub">· ${esc(s.account)}</span>` : ""}
        <span class="history-status ${live}">${s.enabled ? "追更中" : "已关闭"}</span>
        ${s.checking ? '<span class="history-status running">扫描中…</span>' : ""}
      </div>
      <div class="sub-meta">${esc(albumText)} · 已记录 ${s.files_count || 0} 个文件 · 已见 ${s.seen_count || 0} 篇 · 间隔 ${Math.round((s.interval_effective || 600) / 60)} 分钟 · 下次 ${fmtRel(s.next_check)}</div>
      <div class="sub-fields">
        <label class="mini"><span>重命名规则</span><input data-subfield="template" value="${esc(s.template || "")}" placeholder="${esc(s.template_effective || "")}" spellcheck="false"></label>
        <label class="mini"><span>下载目录</span><input data-subfield="dir" value="${esc(s.dir || "")}" placeholder="${esc(s.dir_effective || "")}" spellcheck="false"></label>
        <label class="mini mini-num"><span>间隔（分钟）</span><input data-subfield="interval" type="number" min="0" max="1440" value="${Math.max(0, Math.round((s.interval || 0) / 60))}"></label>
      </div>
      <div class="sub-since">
        <span>下载起点：${s.since
          ? `《${esc(s.since_title || s.since)}》（含${s.since_count ? `，已补下 ${s.since_count} 篇` : ""}）`
          : "未设置（只下载之后的新文章）"}</span>
        <button class="btn tiny ghost" data-subact="pickstart" type="button">选择起点</button>
        ${s.since ? '<button class="btn tiny ghost" data-subact="backfillstart" type="button">按起点补下</button>' : ""}
        ${s.since ? '<button class="btn tiny ghost" data-subact="clearstart" type="button">清除起点</button>' : ""}
      </div>
      <div class="since-picker hidden" data-picker="${esc(s.id)}"></div>
      <div class="sub-acts">
        <button class="btn tiny" data-subact="save" type="button">保存（含改名）</button>
        <button class="btn tiny ghost" data-subact="check" type="button">立即检查</button>
        <button class="btn tiny ghost" data-subact="toggle" type="button">${s.enabled ? "关闭" : "开启"}</button>
        <button class="btn tiny danger" data-subact="delete" type="button">删除</button>
      </div>
      <div class="sub-msg">${esc(msg)}${s.last_error && !msg.includes(s.last_error) ? ` · <span class="err">${esc(s.last_error)}</span>` : ""}</div>
    </div>
  </div>`;
}

async function subAct(id, act, row) {
  const sub = (state.subs ? state.subs.subs : []).find((x) => x.id === id);
  if (!sub) return;
  try {
    if (act === "check") {
      toast("正在扫描…");
      const data = await api("/api/subs/" + encodeURIComponent(id) + "/check", { method: "POST" });
      const res = data.result || {};
      toast(res.message || "扫描完成");
      await loadSubs(true);
      await loadTasks(true);
      return;
    }
    if (act === "toggle") {
      await api("/api/subs", { method: "POST", body: JSON.stringify({ id, enabled: !sub.enabled }) });
      await loadSubs(true);
      return;
    }
    if (act === "pickstart") {
      const box = row.querySelector(".since-picker");
      if (!box) return;
      if (!box.classList.contains("hidden")) { box.classList.add("hidden"); return; }
      box.classList.remove("hidden");
      box.innerHTML = '<div class="hint">正在读取文章列表…</div>';
      const data = await api("/api/subs/" + encodeURIComponent(id) + "/articles?pages=0");
      state.sincePick = { id, list: data.articles || [] };
      renderSincePicker(box, data);
      return;
    }
    if (act === "applystart") {
      const checked = row.querySelector('.since-picker input[type="radio"]:checked');
      if (!checked) { toast("请选择一篇作为起点", true); return; }
      const pick = (state.sincePick && state.sincePick.list) || [];
      const a = pick[Number(checked.value)];
      if (!a) { toast("起点数据失效，请重新打开列表", true); return; }
      await api("/api/subs", {
        method: "POST",
        body: JSON.stringify({
          id, since: a.key, since_title: a.title, since_time: a.publish_time,
          since_order: a.order, since_album: a.album_id,
        }),
      });
      toast(`已设置起点《${a.title}》，正在从这一篇（含）往新文章补下…`);
      await loadSubs(true);
      await loadTasks(true);
      return;
    }
    if (act === "backfillstart") {
      toast("正在按起点补下…");
      const data = await api("/api/subs/" + encodeURIComponent(id) + "/backfill", { method: "POST" });
      toast(data.message || "已建任务");
      await loadSubs(true);
      await loadTasks(true);
      return;
    }
    if (act === "clearstart") {
      await api("/api/subs", { method: "POST", body: JSON.stringify({ id, since: "" }) });
      toast("已清除起点，下次扫描会重新建立基线");
      await loadSubs(true);
      return;
    }
    if (act === "closepicker") {
      const box = row.querySelector(".since-picker");
      if (box) box.classList.add("hidden");
      return;
    }
    if (act === "delete") {
      if (!confirm(`删除追更任务「${sub.name}」？已下载的文件不会被删除。`)) return;
      await api("/api/subs/" + encodeURIComponent(id), { method: "DELETE" });
      toast("已删除追更任务");
      await loadSubs(true);
      return;
    }
    if (act === "save") {
      const tpl = row.querySelector('[data-subfield="template"]').value.trim();
      const dir = row.querySelector('[data-subfield="dir"]').value.trim();
      const minutes = Number(row.querySelector('[data-subfield="interval"]').value) || 0;
      const data = await api("/api/subs", {
        method: "POST",
        body: JSON.stringify({ id, template: tpl, dir, interval: minutes * 60 }),
      });
      const renamed = data.renamed || 0;
      const missing = data.missing || 0;
      toast(`已保存${renamed ? `，${renamed} 个已下载文件已按新规则改名` : ""}${missing ? `；${missing} 个文件已不存在，保持原样` : ""}`);
      await loadSubs(true);
      await loadTasks(true);
    }
  } catch (e) {
    toast(e.message, true);
  }
}

function renderCookieState(c) {
  const el = $("#cookieState");
  if (!el) return;
  if (!c.set) {
    el.textContent = "Cookie：未设置（按篇追更需要）";
    el.classList.add("err");
    return;
  }
  el.classList.remove("err");
  el.textContent = c.uin && c.key
    ? "Cookie：已设置（含 uin/key）"
    : "Cookie：已设置，但缺少 uin 或 key，按篇追更会失败";
  if (!(c.uin && c.key)) el.classList.add("err");
}

async function saveCookie(clear) {
  const input = $("#wxCookie");
  const cookie = clear ? "" : input.value.trim();
  if (!clear && !cookie) { toast("请先粘贴 Cookie", true); return; }
  try {
    const data = await api("/api/settings/cookie", {
      method: "POST",
      body: JSON.stringify({ cookie }),
    });
    if (clear) toast("已清除 Cookie");
    else if (data.uin && data.key) toast("Cookie 已保存（含 uin/key），可以按篇追更了");
    else toast("Cookie 已保存，但没有找到 uin 或 key，按篇追更仍会失败", true);
    input.value = "";
    renderCookieState(data);
    await loadSubs(true);
  } catch (e) {
    toast(e.message, true);
  }
}

async function saveSubConfig() {
  const body = {
    enabled: $("#subEnabled").checked,
    interval: Math.max(2, Number($("#subInterval").value) || 10) * 60,
    max_new: Math.max(1, Number($("#subMaxNew").value) || 10),
    min_gap: Math.max(30, Number($("#subMinGap").value) || 60),
  };
  try {
    const data = await api("/api/subs/config", { method: "POST", body: JSON.stringify(body) });
    state.subs = data;
    renderSubs();
    toast("已保存追更设置");
  } catch (e) { toast(e.message, true); }
}

async function doSubResolve() {
  const q = $("#subUrl").value.trim();
  if (!q) { toast("请先粘贴合集或文章链接", true); return; }
  const btn = $("#btnSubResolve");
  setLoading(btn, true);
  try {
    const data = await api("/api/subs/search", { method: "POST", body: JSON.stringify({ q }) });
    const acc = (data.accounts || [])[0];
    const info = $("#subResolveInfo");
    if (!acc) {
      state.subResolved = null;
      $("#btnSubAdd").disabled = true;
      info.textContent = data.note || "没有解析到公众号";
      return;
    }
    state.subResolved = acc;
    const albums = acc.albums || [];
    const isAlbum = /appmsgalbum/.test(q);
    info.innerHTML = `识别到：<b>${esc(acc.name)}</b>（${esc(acc.biz)}）`
      + (albums.length ? ` · 公开合集 ${albums.length} 个：${albums.map((a) => esc(a.title || a.id)).join("、")}`
        : ' · <span class="err">没有公开合集，微信无法扫描这个公众号的新文章</span>')
      + (data.note ? ` · ${esc(data.note)}` : "");
    const cookie = (state.subs && state.subs.cookie) || {};
    if (!isAlbum && $("#subKind").value === "album") {
      // 文章链接默认追更「该公众号全部文章」（有 Cookie 时），否则退回公开合集。
      $("#subKind").value = cookie.uin && cookie.key ? "author" : "account";
    }
    if (isAlbum) $("#subKind").value = "album";
    $("#btnSubAdd").disabled = isAlbum ? albums.length === 0 : !acc.biz;
    $("#subTemplate").placeholder = $("#subKind").value === "album"
      ? "<collection_title>/<post_id>-<order>.<ext>"
      : "<title>-<post_id>-<order>.<ext>";
  } catch (e) {
    $("#subResolveInfo").textContent = e.message;
    $("#btnSubAdd").disabled = true;
  } finally {
    setLoading(btn, false);
  }
}

async function doSubAdd() {
  const acc = state.subResolved;
  if (!acc) { toast("请先解析链接", true); return; }
  const url = $("#subUrl").value.trim();
  const kind = $("#subKind").value || "album";
  if (kind === "author") {
    const cookie = (state.subs && state.subs.cookie) || {};
    if (!(cookie.uin && cookie.key)) {
      toast("按篇追更需要先在下面填入微信 Cookie（含 uin/key）", true);
      return;
    }
  }
  const body = {
    url,
    kind,
    name: acc.name,
    biz: acc.biz,
    albums: (acc.albums || []).map((a) => ({ id: a.id, title: a.title })),
    template: $("#subTemplate").value.trim(),
    dir: $("#subDir").value.trim(),
    backfill: Number($("#subBackfill").value) || 0,
  };
  const btn = $("#btnSubAdd");
  setLoading(btn, true);
  try {
    const data = await api("/api/subs", { method: "POST", body: JSON.stringify(body) });
    toast("已添加追更：" + (data.sub && data.sub.name ? data.sub.name : ""));
    $("#subUrl").value = "";
    $("#subResolveInfo").textContent = "添加成功，第一次扫描会建立基线（填了补下篇数会下载历史文章）。";
    state.subResolved = null;
    btn.disabled = true;
    await loadSubs(true);
    await loadTasks(true);
  } catch (e) {
    toast(e.message, true);
  } finally {
    setLoading(btn, false);
  }
}

async function doSubSearch() {
  const q = $("#subQuery").value.trim();
  const btn = $("#btnSubSearch");
  setLoading(btn, true);
  try {
    const data = await api("/api/subs/search", { method: "POST", body: JSON.stringify({ q }) });
    renderSubResults(data);
  } catch (e) {
    toast(e.message, true);
  } finally {
    setLoading(btn, false);
  }
}

function renderSubResults(data) {
  const box = $("#subResults");
  const list = data.accounts || [];
  const note = data.note ? `<div class="hint">${esc(data.note)}</div>` : "";
  if (!list.length) {
    box.innerHTML = note || '<div class="empty">没有匹配的公众号</div>';
    return;
  }
  box.innerHTML = list.map((a, i) => {
    const albums = a.albums || [];
    return `<div class="sub-result">
      <div class="sub-result-main">
        <b>${esc(a.name)}</b>
        <span class="sub">${esc(a.gh_id || "")}${a.gh_id ? " · " : ""}${esc(a.biz)}</span>
        <div class="sub">公开合集 ${albums.length} 个${albums.length ? "：" + albums.map((x) => esc(x.title || x.id)).join("、") : "（微信未公开，无法扫描新文章）"}</div>
      </div>
      <span class="row gap">
        <button class="btn tiny ghost" data-addacc="${i}" type="button"${albums.length ? "" : " disabled"}>追更合集</button>
        <button class="btn tiny" data-addauthor="${i}" type="button">追更全部文章</button>
      </span>
    </div>`;
  }).join("") + note;
  box.querySelectorAll("[data-addacc]").forEach((btn) => {
    btn.addEventListener("click", () => addAccountSub(list[Number(btn.dataset.addacc)]));
  });
  box.querySelectorAll("[data-addauthor]").forEach((btn) => {
    btn.addEventListener("click", () => addAuthorSub(list[Number(btn.dataset.addauthor)]));
  });
}

async function addAccountSub(acc) {
  if (!acc) return;
  const body = {
    kind: "account",
    biz: acc.biz,
    name: acc.name,
    albums: (acc.albums || []).map((a) => ({ id: a.id, title: a.title })),
    template: $("#subTemplate").value.trim(),
    dir: $("#subDir").value.trim(),
    backfill: Number($("#subBackfill").value) || 0,
  };
  try {
    const data = await api("/api/subs", { method: "POST", body: JSON.stringify(body) });
    toast("已添加追更：" + (data.sub && data.sub.name ? data.sub.name : ""));
    await loadSubs(true);
    await loadTasks(true);
  } catch (e) {
    toast(e.message, true);
  }
}

function renderSincePicker(box, data) {
  const list = data.articles || [];
  if (!list.length) {
    box.innerHTML = '<div class="hint">没有读取到文章列表' + (data.warning ? "：" + esc(data.warning) : "") + "</div>";
    return;
  }
  const multiAlbum = new Set(list.map((a) => a.album_id)).size > 1;
  const rows = list.map((a, i) => {
    const when = a.publish_time ? new Date(a.publish_time * 1000).toLocaleDateString("zh-CN") : "";
    const meta = [when, multiAlbum ? a.album_title : "", a.seen ? "已见" : ""].filter(Boolean).join(" · ");
    return `<label class="since-item${a.current ? " current" : ""}">
      <input type="radio" name="since-${esc(data.sub_id)}" value="${i}"${a.current ? " checked" : ""}>
      <span class="since-order">${a.order || ""}</span>
      <span class="since-title">${esc(a.title)}</span>
      <span class="since-meta">${esc(meta)}</span>
    </label>`;
  }).join("");
  box.innerHTML = `<div class="since-head">共 ${list.length} 篇（按时间倒序）。选一篇作为起点：<b>包含这一篇</b>，向更新的方向下载</div>
    <div class="since-list">${rows}</div>
    <div class="row gap wrap-row">
      <button class="btn tiny" data-subact="applystart" type="button">确定从这里开始</button>
      <button class="btn tiny ghost" data-subact="closepicker" type="button">取消</button>
      ${data.warning ? `<span class="err">${esc(data.warning)}</span>` : ""}
    </div>`;
}

async function addAuthorSub(acc) {
  if (!acc) return;
  const cookie = (state.subs && state.subs.cookie) || {};
  if (!(cookie.uin && cookie.key)) {
    toast("按篇追更需要先在「微信 Cookie」里填入含 uin/key 的 Cookie", true);
    return;
  }
  try {
    const data = await api("/api/subs", {
      method: "POST",
      body: JSON.stringify({
        kind: "author",
        biz: acc.biz,
        name: acc.name,
        template: $("#subTemplate").value.trim(),
        dir: $("#subDir").value.trim(),
        backfill: Number($("#subBackfill").value) || 0,
      }),
    });
    toast("已添加按篇追更：" + (data.sub && data.sub.name ? data.sub.name : ""));
    await loadSubs(true);
    await loadTasks(true);
  } catch (e) {
    toast(e.message, true);
  }
}

async function subBatch(action) {
  const ids = [...state.subSel];
  if (!ids.length) { toast("请先勾选追更任务", true); return; }
  if (action === "delete" && !confirm(`删除 ${ids.length} 个追更任务？已下载的文件不会被删除。`)) return;
  try {
    const data = await api("/api/subs/batch", { method: "POST", body: JSON.stringify({ action, ids }) });
    const word = { enable: "开启", disable: "关闭", delete: "删除", check: "检查" }[action] || action;
    toast(`已${word} ${data.count || 0} 个追更任务`);
    state.subSel.clear();
    await loadSubs(true);
    await loadTasks(true);
  } catch (e) {
    toast(e.message, true);
  }
}

init();
