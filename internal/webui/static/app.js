"use strict";

const $ = (sel, root = document) => root.querySelector(sel);
const KEY = "wxmp.settings.v1";
const JOB_KEY = "wxmp.job.v1";

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
    state.running = job.status === "running";
    state.logCount = 0;
    state.resultCount = 0;
    $("#logs").textContent = "";
    $("#results").innerHTML = '<div class="empty">等待结果…</div>';
    $("#cardProgress").classList.remove("hidden");
    $("#btnCancel").classList.toggle("hidden", !state.running);
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

const HISTORY_LABEL = { running: "进行中", done: "完成", failed: "失败", canceled: "已取消" };

function historyTime(ts) {
  if (!ts) return "";
  const d = new Date(ts * 1000);
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function renderHistory(jobs) {
  const list = $("#historyList");
  if (!list) return;
  list.textContent = "";
  if (!jobs || !jobs.length) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "暂无历史任务";
    list.appendChild(empty);
    return;
  }
  jobs.forEach((j) => {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "history-row";
    const status = HISTORY_LABEL[j.status] || j.status || "未知";
    const url = (j.urls && j.urls[0]) || j.out_dir || "";
    const meta = `共 ${j.total || 0} 项 · 成功 ${j.success || 0} · 跳过 ${j.skipped || 0} · 失败 ${j.failed || 0}`;
    row.innerHTML = `<span class="history-status ${esc(j.status)}">${esc(status)}</span>
      <span class="history-main"><span class="history-url"></span><span class="history-meta"></span></span>
      <span class="history-time">${esc(historyTime(j.started_at || j.finished_at))}</span>`;
    row.querySelector(".history-url").textContent = url;
    row.querySelector(".history-meta").textContent = meta + (j.error ? ` · ${j.error}` : "");
    row.addEventListener("click", () => openJob(j.id));
    list.appendChild(row);
  });
}

async function loadHistory() {
  try {
    const data = await api("/api/jobs");
    renderHistory(data.jobs || []);
  } catch (_) { /* 历史接口失败不影响下载 */ }
}

async function openJob(id) {
  if (!id) return;
  try {
    const job = await api("/api/jobs/" + encodeURIComponent(id));
    if (state.timer) { clearTimeout(state.timer); state.timer = null; }
    state.jobId = id;
    state.running = job.status === "running";
    state.logCount = 0;
    state.resultCount = 0;
    saveJob(id);
    $("#logs").textContent = "";
    $("#results").innerHTML = '<div class="empty">等待结果…</div>';
    $("#cardProgress").classList.remove("hidden");
    $("#btnCancel").classList.toggle("hidden", !state.running);
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
  bindEvents();
  const reload = $("#btnHistoryReload");
  if (reload) reload.addEventListener("click", loadHistory);
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
  loadHistory();
  setInterval(checkHealth, 60000);

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
  return `<article class="article collection" data-collection="${ci}">
    <div class="article-head">
      <div>
        <div class="article-title">📚 ${esc(c.title || "未命名合集")}</div>
        <div class="article-meta">${esc(c.account || "")} · id ${esc(c.id)} · ${c.article_count || articles.length} 篇</div>
      </div>
      <div class="article-counts"><span class="tag">合集 ${articles.length}</span></div>
    </div>
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

  return `<article class="article${failed ? " failed" : ""}" data-article="${ai}">
    <div class="article-head">
      <div>
        <div class="article-title">${esc(a.title || "未命名文章")}</div>
        <div class="article-meta">${meta}${a.author && a.author !== a.account ? " · " + esc(a.author) : ""}</div>
      </div>
      <div class="article-counts">${tags.join("")}</div>
    </div>
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
  btn.disabled = state.running || (total === 0 && arts === 0) || state.nameInvalid;
  const label = total && arts ? `开始下载（${total} 项 + 合集 ${arts} 篇）`
    : arts ? `开始下载（合集 ${arts} 篇）`
    : total ? `开始下载（${total} 项）` : "开始下载";
  btn.textContent = state.running ? "下载中…" : label;
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

async function doDownload() {
  if (state.running) return;
  const urls = state.articles
    .filter((a) => a.medias && a.medias.length)
    .map((a) => a.final_url || a.url)
    .concat(state.collections
      .filter((c) => (c.articles || []).length)
      .map((c) => c.final_url || c.url));
  if (!urls.length) {
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
        urls,
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
    renderJob(job);
    if (job.status === "running") {
      state.timer = setTimeout(pollJob, 700);
      return;
    }
    finishJob(job);
  } catch (e) {
    toast("获取进度失败：" + e.message, true);
    state.timer = setTimeout(pollJob, 1500);
  }
}

function renderJob(job) {
  const total = job.total || 0;
  const done = job.done || 0;
  const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : (job.status === "running" ? 3 : 0);
  $("#bar").style.width = pct + "%";
  $("#bar").className = "bar" + (job.status === "failed" ? " failed" : job.status === "canceled" ? " canceled" : "");
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
  loadHistory();
  $("#btnCancel").classList.add("hidden");
  $("#btnDownload").classList.remove("loading");
  $("#btnDownload").disabled = false;
  const map = { done: "完成", canceled: "已取消", failed: "失败" };
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

init();
