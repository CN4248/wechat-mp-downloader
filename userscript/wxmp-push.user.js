// ==UserScript==
// @name         微信公众号图片下载器 · 推送终端版（wxmp push）
// @namespace    https://github.com/wxmp
// @version      1.2.0
// @description  把当前公众号文章/合集里选中的文章，或正文里点选的图片/动图/视频推送到本机（或 NAS）上的 wxmp 服务下载
// @author       wxmp
// @match        https://mp.weixin.qq.com/s/*
// @match        https://mp.weixin.qq.com/s?*
// @match        https://mp.weixin.qq.com/mp/appmsgalbum*
// @grant        GM_xmlhttpRequest
// @grant        GM_setValue
// @grant        GM_getValue
// @grant        GM_registerMenuCommand
// @connect      127.0.0.1
// @connect      localhost
// @connect      *
// @run-at       document-idle
// ==/UserScript==

(function () {
  "use strict";

  const CFG_KEY = "wxmp.push.v1";
  const DEFAULT_SERVER = "http://127.0.0.1:2178";
  const DEFAULT_OUT = "";
  const DEFAULT_TEMPLATE = "";

  function loadCfg() {
    try { return JSON.parse(GM_getValue(CFG_KEY, "{}")) || {}; } catch (_) { return {}; }
  }
  function saveCfg(c) { try { GM_setValue(CFG_KEY, JSON.stringify(c)); } catch (_) {} }
  const cfg = Object.assign({ server: DEFAULT_SERVER, outDir: DEFAULT_OUT, template: DEFAULT_TEMPLATE }, loadCfg());
  if (cfg.outDir === "undefined") cfg.outDir = "";
  if (cfg.template === "undefined") cfg.template = "";

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  /* ---------- 媒体地址（与独立版 / 服务端解析同一套规则） ---------- */

  function decodeEntities(s) {
    return String(s == null ? "" : s)
      .replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">")
      .replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&apos;/g, "'")
      .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
      .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(parseInt(d, 10)))
      .replace(/\\x26/g, "&").replace(/\\\//g, "/");
  }

  function normalizeExt(ext) {
    let e = String(ext || "").toLowerCase().trim().replace(/^\./, "");
    if (e === "jpeg") e = "jpg";
    if (e === "svg+xml") e = "svg";
    return e || "jpg";
  }

  function extFromURL(raw, dataType) {
    const low = String(raw || "").toLowerCase();
    const wx = low.match(/[?&]wx_fmt=([a-z0-9]+)/);
    if (wx) {
      const v = wx[1];
      if (v === "jpeg" || v === "jpg") return "jpg";
      if (["png", "gif", "webp", "bmp", "svg", "mp4"].includes(v)) return v;
    }
    const t = String(dataType || "").toLowerCase();
    if (t === "jpeg" || t === "jpg") return "jpg";
    if (["png", "gif", "webp", "bmp", "mp4"].includes(t)) return t;
    for (const kw of ["mmbiz_gif", "mmbiz_png", "mmbiz_jpg", "mmbiz_webp"]) {
      if (low.includes(kw)) return kw.slice(6);
    }
    const m = low.match(/\.(png|jpe?g|gif|webp|bmp|mp4)(?:[?#]|$)/);
    if (m) return normalizeExt(m[1]);
    return "jpg";
  }

  const SIZE_SEG = /^\d{2,4}$/;

  function originalURL(raw, ext) {
    let s = decodeEntities(String(raw || "").trim());
    if (s.startsWith("//")) s = "https:" + s;
    if (!/^https?:\/\//i.test(s)) return "";
    let u;
    try { u = new URL(s); } catch (_) { return ""; }
    const parts = u.pathname.split("/");
    if (parts.length && SIZE_SEG.test(parts[parts.length - 1])) parts[parts.length - 1] = "0";
    u.pathname = parts.join("/");
    const e = normalizeExt(ext);
    if (e && e !== "bin") u.searchParams.set("wx_fmt", e === "jpg" ? "jpeg" : e);
    u.searchParams.delete("tp");
    return u.toString();
  }

  function cdnKey(raw) {
    try {
      const u = new URL(decodeEntities(String(raw || "")));
      const parts = u.pathname.replace(/^\/+|\/+$/g, "").split("/");
      if (parts.length && /^\d{1,4}$/.test(parts[parts.length - 1])) parts.pop();
      const last = parts[parts.length - 1] || "";
      if (last.length >= 24) return "h:" + last.toLowerCase();
      return (u.host || "").toLowerCase() + "/" + parts.join("/");
    } catch (_) {
      return String(raw || "");
    }
  }

  function api(method, path, body) {
    const url = cfg.server.replace(/\/+$/, "") + path;
    return new Promise((resolve, reject) => {
      GM_xmlhttpRequest({
        method,
        url,
        timeout: 30000,
        headers: { "Content-Type": "application/json" },
        data: body ? JSON.stringify(body) : undefined,
        onload: (r) => {
          let data = null;
          try { data = JSON.parse(r.responseText); } catch (_) {}
          if (r.status >= 200 && r.status < 300) resolve(data);
          else reject(new Error((data && data.error) || ("HTTP " + r.status)));
        },
        onerror: () => reject(new Error("无法连接 " + cfg.server + "，请确认服务已启动")),
        ontimeout: () => reject(new Error("连接超时")),
      });
    });
  }

  let shadow, panel, statusEl, logEl, barEl, serverInput, pushBtn;
  let pickToggle, pickClear, pickBtn, pickBar;
  let jobTimer = null;
  let busy = false;

  function log(msg, err) {
    if (!logEl) return;
    const d = el("div", "", msg);
    if (err) d.style.color = "#ff9b9b";
    logEl.appendChild(d);
    logEl.scrollTop = logEl.scrollHeight;
  }

  function setStatus(s) { if (statusEl) statusEl.textContent = s; }

  function mount() {
    const host = el("div");
    host.id = "wxmp-push-host";
    host.style.cssText = "position:fixed;z-index:2147483000;right:0;bottom:0;width:0;height:0;";
    document.documentElement.appendChild(host);
    shadow = host.attachShadow({ mode: "open" });
    const style = el("style");
    style.textContent = `
      :host { all: initial; }
      .fab { position:fixed; right:22px; bottom:96px; border:0; cursor:pointer; padding:11px 16px; border-radius:999px;
        background:linear-gradient(145deg,#3b82f6,#1d4ed8); color:#fff; font:600 13px/1 "PingFang SC","Microsoft YaHei",sans-serif;
        box-shadow:0 12px 30px -10px rgba(29,78,216,.7); }
      .panel { position:fixed; right:22px; bottom:158px; width:352px; background:#fff; border:1px solid #dfe7f2; border-radius:16px;
        box-shadow:0 24px 60px -20px rgba(15,35,70,.45); font:13px/1.55 "PingFang SC","Microsoft YaHei",sans-serif; color:#16233b; display:none; }
      .panel.open { display:block; }
      .hd { display:flex; align-items:center; gap:8px; padding:12px 14px; border-bottom:1px solid #eef2f8; font-weight:700; }
      .hd .x { margin-left:auto; border:0; background:#f2f5fa; width:26px; height:26px; border-radius:50%; cursor:pointer; color:#55637a; }
      .bd { padding:13px 14px 14px; }
      label.sm { display:block; font-size:11.5px; color:#55637a; font-weight:600; margin:9px 0 4px; }
      input { width:100%; border:1px solid #dfe7f2; border-radius:8px; padding:8px 9px; font:12px/1.4 Consolas,monospace; outline:none; }
      button.p { width:100%; margin-top:10px; border:0; border-radius:9px; padding:10px 12px; background:linear-gradient(145deg,#3b82f6,#1d4ed8);
        color:#fff; font:600 12.5px/1 inherit; font-family:inherit; cursor:pointer; }
      button.p:disabled { opacity:.55; cursor:not-allowed; }
      button.g { border:1px solid #dfe7f2; background:#fff; border-radius:9px; padding:8px 10px; color:#55637a; font:600 12px/1 inherit; font-family:inherit; cursor:pointer; }
      .row { display:flex; gap:8px; margin-top:8px; }
      .prog { height:6px; border-radius:999px; background:#eef2f8; overflow:hidden; margin-top:10px; }
      .prog i { display:block; height:100%; width:0; background:linear-gradient(90deg,#60a5fa,#2563eb); transition:width .3s; }
      .status { margin-top:6px; font-size:11.5px; color:#6b7a90; min-height:17px; }
      .log { margin-top:8px; max-height:120px; overflow:auto; background:#0f172a; color:#dbeafe; border-radius:8px; padding:7px 9px;
        font:10.5px/1.65 Consolas,monospace; white-space:pre-wrap; word-break:break-all; }
      .foot { margin-top:8px; font-size:10.5px; color:#93a1b5; }
      .chk { display:flex; align-items:center; gap:6px; margin-top:10px; font-size:12px; color:#55637a; }
      .chk input { width:auto; accent-color:#2563eb; }
      .pickbar { margin-top:8px; padding:9px; border:1px dashed #c7dcf7; border-radius:10px; background:#f7faff; }
      .pickbar button.p { margin-top:0; }
      .pickbar button.g { width:100%; margin-top:6px; }
      .pickhint { margin-top:7px; font-size:10.5px; color:#8194ad; line-height:1.5; }
    `;
    shadow.appendChild(style);

    const fab = el("button", "fab", "推送到终端");
    fab.type = "button";
    fab.addEventListener("click", togglePanel);
    shadow.appendChild(fab);

    panel = el("div", "panel");
    panel.innerHTML = `
      <div class="hd">推送到终端下载<button class="x" type="button" title="关闭">×</button></div>
      <div class="bd">
        <label class="sm">服务地址（Docker / exe 的 WebUI）</label>
        <input type="text" data-server spellcheck="false" placeholder="http://127.0.0.1:2178">
        <label class="sm">服务端下载目录（留空=服务端默认；Docker 填 /download）</label>
        <input type="text" data-outdir spellcheck="false" placeholder="留空使用服务端默认目录">
        <label class="sm">服务端重命名规则（留空=服务端默认）</label>
        <input type="text" data-tpl spellcheck="false" placeholder="留空使用服务端默认命名">
        <div class="row"><button class="g" data-test type="button" style="flex:1">测试连接</button><button class="g" data-save type="button" style="flex:1">保存设置</button></div>
        <label class="chk"><input type="checkbox" data-pick> 多选模式（点选正文图片 / 动图 / 视频）</label>
        <div class="pickbar" data-pickbar style="display:none">
          <button class="p" data-picked type="button" disabled>推送所选（0 项）</button>
          <button class="g" data-pickclear type="button">清空选择</button>
          <div class="pickhint">选中后把媒体地址推给服务端，服务端按文章的命名规则下载；文件名与整篇下载一致。</div>
        </div>
        <div class="prog"><i data-bar></i></div>
        <div class="status" data-status>把当前文章推送到服务端下载</div>
        <button class="p" data-push type="button">推送当前文章</button>
        <div class="log" data-log></div>
        <div class="foot">脚本只发送文章/媒体地址，文件由服务端下载；与“独立运行版”互不影响。</div>
      </div>`;
    shadow.appendChild(panel);

    serverInput = panel.querySelector("[data-server]");
    serverInput.value = cfg.server;
    panel.querySelector("[data-outdir]").value = cfg.outDir;
    panel.querySelector("[data-tpl]").value = cfg.template;
    statusEl = panel.querySelector("[data-status]");
    logEl = panel.querySelector("[data-log]");
    barEl = panel.querySelector("[data-bar]");
    pushBtn = panel.querySelector("[data-push]");

    panel.querySelector(".x").addEventListener("click", () => panel.classList.remove("open"));
    panel.querySelector("[data-save]").addEventListener("click", () => {
      cfg.server = serverInput.value.trim() || DEFAULT_SERVER;
      cfg.outDir = panel.querySelector("[data-outdir]").value.trim();
      cfg.template = panel.querySelector("[data-tpl]").value.trim();
      saveCfg(cfg);
      setStatus("已保存：" + cfg.server + " → " + cfg.outDir);
    });
    panel.querySelector("[data-test]").addEventListener("click", async () => {
      cfg.server = serverInput.value.trim() || DEFAULT_SERVER;
      saveCfg(cfg);
      try {
        const h = await api("GET", "/api/health");
        setStatus("连接正常：" + (h.app || "wxmp"));
        log("✓ 服务可用：" + cfg.server);
      } catch (e) { setStatus(e.message); log("✗ " + e.message, true); }
    });
    pushBtn.addEventListener("click", pushCurrent);

    pickToggle = panel.querySelector("[data-pick]");
    pickClear = panel.querySelector("[data-pickclear]");
    pickBtn = panel.querySelector("[data-picked]");
    pickBar = panel.querySelector("[data-pickbar]");
    pickToggle.addEventListener("change", () => setPickMode(pickToggle.checked));
    pickClear.addEventListener("click", () => { clearPicks(); pickHint(); });
    pickBtn.addEventListener("click", pushPicked);
    window.addEventListener("click", onPickClick, true);

    GM_registerMenuCommand("打开推送面板", togglePanel);
  }

  function togglePanel() {
    panel.classList.toggle("open");
    if (panel.classList.contains("open")) serverInput.focus();
  }

  function overrides() {
    const body = {};
    if (cfg.outDir) body.out_dir = cfg.outDir;
    if (cfg.template) body.name_template = cfg.template;
    return body;
  }

  function payload(urls) {
    return Object.assign({ urls }, overrides());
  }

  async function pushURLs(urls) {
    if (busy) return;
    busy = true;
    pushBtn.disabled = true;
    updatePickUI();
    if (barEl) barEl.style.width = "8%";
    setStatus("正在提交 " + urls.length + " 个链接…");
    logEl.innerHTML = "";
    try {
      const data = await api("POST", "/api/download", payload(urls));
      const job = data && data.job;
      if (!job) throw new Error("服务端没有返回任务");
      log("已创建任务 " + job.id);
      setStatus("任务已提交，等待服务端下载…");
      pollJob(job.id);
    } catch (e) {
      setStatus(e.message);
      log("✗ " + e.message, true);
      busy = false;
      if (pushBtn) pushBtn.disabled = false;
      if (barEl) barEl.style.width = "0";
      updatePickUI();
    }
  }

  async function pollJob(id) {
    if (jobTimer) clearTimeout(jobTimer);
    try {
      const d = await api("GET", "/api/jobs/" + id);
      const total = d.total || 0;
      const done = d.done || 0;
      if (barEl) barEl.style.width = (total ? Math.round((done / total) * 100) : 5) + "%";
      setStatus(`${d.status || "running"} · ${done}/${total || "?"} · 成功 ${d.success || 0}，跳过 ${d.skipped || 0}，失败 ${d.failed || 0}`);
      (d.logs || []).slice(-6).forEach((l) => {
        if (l.text && !logEl.textContent.includes(l.text)) log(l.text, /^✗/.test(l.text));
      });
      if (d.status === "done" || d.status === "failed" || d.status === "canceled") {
        busy = false;
        if (pushBtn) pushBtn.disabled = false;
        updatePickUI();
        log(d.status === "done" ? "✓ 下载完成" : "✗ 任务结束：" + (d.error || d.status), d.status !== "done");
        return;
      }
      jobTimer = setTimeout(() => pollJob(id), 1800);
    } catch (e) {
      jobTimer = setTimeout(() => pollJob(id), 3000);
    }
  }

  async function pushCurrent() {
    if (busy) return;
    setStatus("正在读取当前页面…");
    const urls = [location.href];
    await pushURLs(urls);
  }

  /* ---------- 正文媒体多选（推送所选） ---------- */

  const PICK_SEL = "img, video, mpvideo, mp-common-videosnap";
  const PICK_STYLE_ID = "wxmp-pick-style";

  let pickMode = false;
  const pickPicks = new Map(); // element -> media info
  const pickFrames = new Map(); // element -> overlay frame
  let pickLayer = null, pickFramesBox = null, pickTimer = null;

  function pickContentRoot() { return document.querySelector("#js_content"); }

  function pickVideoURL(node) {
    const tag = node.tagName.toLowerCase();
    let cand = "";
    if (tag === "video") {
      cand = node.currentSrc || node.src || "";
    } else {
      cand = node.getAttribute("data-url") || node.getAttribute("data-src") || node.getAttribute("src") || "";
      if (!/\.mp4([?#]|$)/i.test(cand)) {
        const m = String(node.outerHTML || "").match(/https?:\/\/[^\s"'<>\\]+?\.mp4[^\s"'<>\\]*/i);
        cand = m ? m[0] : "";
      }
      if (!cand) {
        const v = node.querySelector && node.querySelector("video");
        cand = v ? (v.currentSrc || v.src || "") : "";
      }
    }
    cand = decodeEntities(String(cand || "").trim());
    if (cand.startsWith("//")) cand = "https:" + cand;
    if (!/^https?:\/\//i.test(cand)) return "";
    return cand.replace(/\\\//g, "/");
  }

  function pickInfoOf(node) {
    if (!node) return null;
    const tag = node.tagName.toLowerCase();
    if (tag === "img") {
      const raw = node.getAttribute("data-src") || node.getAttribute("src") || "";
      if (!raw || /^data:/i.test(raw)) return null;
      if (!/qpic\.cn|qlogo\.cn|mmbiz|wx_fmt=/i.test(raw)) return null;
      const type = (node.getAttribute("data-type") || "").toLowerCase();
      let ext = extFromURL(raw, type);
      const isGIF = type === "gif" || ext === "gif" || /mmbiz_gif/i.test(raw);
      if (isGIF) ext = "gif";
      const url = originalURL(raw, ext);
      if (!url) return null;
      const w = Number(node.getAttribute("data-w")) || Number(node.getAttribute("width")) || 0;
      const ratio = Number(node.getAttribute("data-ratio")) || 0;
      const h = Number(node.getAttribute("height")) || (w > 0 && ratio > 0 ? Math.round(w * ratio) : 0);
      return {
        url, ext, kind: isGIF ? "gif" : "image", width: w, height: h, is_gif: isGIF,
        file_id: node.getAttribute("data-imgfileid") || "",
      };
    }
    const vurl = pickVideoURL(node);
    if (!vurl) return null;
    return { url: vurl, ext: "mp4", kind: "video", width: 0, height: 0, is_gif: false, file_id: "" };
  }

  function pickMediaFromTarget(target) {
    if (!target || !target.closest) return null;
    const node = target.closest(PICK_SEL);
    if (!node) return null;
    const root = pickContentRoot();
    if (!root || !root.contains(node)) return null;
    return node;
  }

  function onPickClick(e) {
    if (!pickMode || busy) return;
    const node = pickMediaFromTarget(e.target);
    if (!node) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.stopImmediatePropagation) e.stopImmediatePropagation();
    togglePick(node);
  }

  function ensurePickPageStyle() {
    if (document.getElementById(PICK_STYLE_ID)) return;
    const style = document.createElement("style");
    style.id = PICK_STYLE_ID;
    style.textContent =
      "html.wxmp-pick-on #js_content img,html.wxmp-pick-on #js_content video," +
      "html.wxmp-pick-on #js_content mpvideo,html.wxmp-pick-on #js_content mp-common-videosnap{cursor:crosshair !important;}";
    (document.head || document.documentElement).appendChild(style);
  }

  function ensurePickLayer() {
    if (pickLayer && pickLayer.isConnected) return;
    const host = el("div");
    host.id = "wxmp-pick-host";
    host.style.cssText = "position:fixed;inset:0;z-index:2147482000;pointer-events:none;";
    document.documentElement.appendChild(host);
    const sh = host.attachShadow({ mode: "open" });
    const style = el("style");
    style.textContent = `
      :host { all: initial; }
      .frames { position:absolute; inset:0; pointer-events:none; }
      .frame { position:absolute; display:none; box-sizing:border-box; border:3px solid #2563eb; border-radius:7px;
        box-shadow:0 0 0 2px rgba(255,255,255,.92), 0 10px 24px rgba(37,99,235,.35); pointer-events:none; }
      .tag { position:absolute; left:-3px; top:-25px; padding:4px 7px; border-radius:6px; background:#2563eb; color:#fff;
        font:600 11px/1 "PingFang SC","Microsoft YaHei",sans-serif; white-space:nowrap; }
    `;
    sh.appendChild(style);
    pickFramesBox = el("div", "frames");
    sh.appendChild(pickFramesBox);
    pickLayer = host;
    window.addEventListener("scroll", syncPickFrames, true);
    window.addEventListener("resize", syncPickFrames);
  }

  function drawPickFrame(node) {
    ensurePickLayer();
    if (pickFrames.has(node)) return;
    const frame = el("div", "frame");
    frame.appendChild(el("span", "tag", "✓ 已选"));
    pickFramesBox.appendChild(frame);
    pickFrames.set(node, frame);
  }

  function syncPickFrames() {
    if (!pickMode || !pickFrames.size) return;
    pickFrames.forEach((frame, node) => {
      if (!node.isConnected) { frame.style.display = "none"; return; }
      const r = node.getBoundingClientRect();
      const visible = r.width > 2 && r.height > 2 && r.bottom > 0 && r.right > 0 &&
        r.top < window.innerHeight && r.left < window.innerWidth;
      if (!visible) { frame.style.display = "none"; return; }
      frame.style.display = "block";
      frame.style.left = Math.round(r.left) + "px";
      frame.style.top = Math.round(r.top) + "px";
      frame.style.width = Math.round(r.width) + "px";
      frame.style.height = Math.round(r.height) + "px";
    });
  }

  function togglePick(node) {
    if (pickPicks.has(node)) { removePick(node); return; }
    const info = pickInfoOf(node);
    if (!info) { setStatus("这个媒体暂时拿不到可下载地址"); return; }
    pickPicks.set(node, info);
    drawPickFrame(node);
    syncPickFrames();
    updatePickUI();
    pickHint();
  }

  function removePick(node) {
    const frame = pickFrames.get(node);
    if (frame && frame.parentNode) frame.parentNode.removeChild(frame);
    pickFrames.delete(node);
    pickPicks.delete(node);
    updatePickUI();
    pickHint();
  }

  function clearPicks() {
    pickFrames.forEach((frame) => { if (frame.parentNode) frame.parentNode.removeChild(frame); });
    pickFrames.clear();
    pickPicks.clear();
    updatePickUI();
  }

  function setPickMode(on) {
    pickMode = !!on;
    if (pickToggle) pickToggle.checked = pickMode;
    if (pickBar) pickBar.style.display = pickMode ? "block" : "none";
    document.documentElement.classList.toggle("wxmp-pick-on", pickMode);
    ensurePickPageStyle();
    if (pickMode) {
      ensurePickLayer();
      syncPickFrames();
      if (!pickTimer) pickTimer = setInterval(syncPickFrames, 160);
    } else {
      clearPicks();
      if (pickTimer) { clearInterval(pickTimer); pickTimer = null; }
      setStatus("多选模式已关闭");
    }
    updatePickUI();
    pickHint();
  }

  function pickHint() {
    if (!pickMode || busy) return;
    const n = pickPicks.size;
    setStatus(n ? `已选 ${n} 项，点“推送所选”发给服务端` : "多选模式：点击正文里的图片 / 动图 / 视频进行选择");
  }

  function updatePickUI() {
    const n = pickPicks.size;
    if (pickBtn) {
      pickBtn.textContent = `推送所选（${n} 项）`;
      pickBtn.disabled = n === 0 || busy;
    }
  }

  async function pushPicked() {
    if (busy) return;
    const picks = Array.from(pickPicks.values());
    if (!picks.length) { setStatus("请先点选正文里的媒体"); return; }
    const seen = new Set();
    const items = [];
    picks.forEach((p) => {
      const key = cdnKey(p.url);
      if (seen.has(key)) return;
      seen.add(key);
      items.push({ url: p.url, ext: p.ext, kind: p.kind, width: p.width || 0, height: p.height || 0, is_gif: !!p.is_gif });
    });
    if (!items.length) { setStatus("选中的媒体没有可用地址"); return; }

    busy = true;
    pushBtn.disabled = true;
    updatePickUI();
    if (barEl) barEl.style.width = "8%";
    setStatus("正在推送 " + items.length + " 项选中媒体…");
    logEl.innerHTML = "";
    try {
      const body = Object.assign({ pick: { page_url: location.href, picked: items } }, overrides());
      const data = await api("POST", "/api/download/media", body);
      const job = data && data.job;
      if (!job) throw new Error("服务端没有返回任务");
      log("已推送 " + items.length + " 项 → 任务 " + job.id);
      setStatus("任务已提交，等待服务端下载…");
      pollJob(job.id);
    } catch (e) {
      const msg = /HTTP 404/.test(e.message)
        ? "服务端不支持“推送选中媒体”（HTTP 404），请更新 Docker 镜像 / exe 后重试"
        : e.message;
      setStatus(msg);
      log("✗ " + msg, true);
      busy = false;
      if (pushBtn) pushBtn.disabled = false;
      if (barEl) barEl.style.width = "0";
      updatePickUI();
    }
  }

  /* ---------- 合集页多选 ---------- */

  function isAlbumPage() { return /\/mp\/appmsgalbum/.test(location.pathname); }
  function albumItems() { return Array.from(document.querySelectorAll("li.js_album_item")); }
  function itemURL(li) {
    const raw = li.getAttribute("data-link") || (li.querySelector("a[href]") || {}).href || "";
    try { const u = new URL(raw, location.href); u.hash = ""; return u.toString(); } catch (_) { return ""; }
  }

  let multi = false, albumBtn = null, albumBar = null, albumCount = null, albumStatus = null;

  function selected() {
    return albumItems().filter((li) => {
      const cb = li.querySelector(".wxmp-pick-push");
      return cb && cb.checked;
    });
  }

  function syncCount() {
    if (albumCount) albumCount.textContent = String(selected().length);
    if (albumBtn) albumBtn.textContent = `批量推送所选（${selected().length} 篇）`;
    if (albumBtn) albumBtn.disabled = selected().length === 0 || busy;
  }

  function decorate() {
    albumItems().forEach((li) => {
      if (li.querySelector(".wxmp-pick-pushwrap")) return;
      li.style.position = "relative";
      const wrap = el("label", "wxmp-pick-pushwrap");
      wrap.style.cssText = "position:absolute;top:8px;right:8px;z-index:20;display:" + (multi ? "flex" : "none") +
        ";align-items:center;justify-content:center;width:26px;height:26px;border-radius:50%;background:rgba(255,255,255,.94);border:1px solid #2563eb;cursor:pointer;";
      const cb = el("input", "wxmp-pick-push");
      cb.type = "checkbox";
      cb.style.cssText = "width:16px;height:16px;accent-color:#2563eb;cursor:pointer;margin:0;";
      cb.addEventListener("change", () => {
        wrap.style.background = cb.checked ? "#2563eb" : "rgba(255,255,255,.94)";
        cb.style.accentColor = "#fff";
        li.style.outline = cb.checked ? "2px solid #2563eb" : "";
        li.style.outlineOffset = "-2px";
        syncCount();
      });
      wrap.appendChild(cb);
      li.appendChild(wrap);
      li.addEventListener("click", (e) => {
        if (!multi) return;
        if (e.target.closest(".wxmp-pick-pushwrap")) return;
        e.preventDefault();
        e.stopPropagation();
        cb.checked = !cb.checked;
        cb.dispatchEvent(new Event("change"));
      }, true);
    });
    syncCount();
  }

  function setMulti(on) {
    multi = !!on;
    albumItems().forEach((li) => {
      const w = li.querySelector(".wxmp-pick-pushwrap");
      if (w) w.style.display = multi ? "flex" : "none";
      if (!multi) {
        const cb = li.querySelector(".wxmp-pick-push");
        if (cb) { cb.checked = false; cb.dispatchEvent(new Event("change")); }
      }
    });
    if (albumBar) albumBar.querySelector(".toggle").textContent = multi ? "退出多选模式" : "进入多选模式";
    if (albumBtn) albumBtn.style.display = multi ? "block" : "none";
    if (albumStatus) albumStatus.textContent = multi ? "点击文章卡片勾选，再批量推送。" : "多选模式已关闭。";
    decorate();
  }

  function mountAlbumBar() {
    const host = el("div");
    host.id = "wxmp-push-album-host";
    host.style.cssText = "position:fixed;z-index:2147483000;right:0;bottom:0;width:0;height:0;";
    document.documentElement.appendChild(host);
    const sh = host.attachShadow({ mode: "open" });
    const style = el("style");
    style.textContent = `
      :host { all: initial; }
      .album-fab { position:fixed; right:22px; bottom:96px; border:0; cursor:pointer; padding:11px 16px;
        border-radius:999px; background:linear-gradient(145deg,#3b82f6,#1d4ed8); color:#fff;
        font:600 13px/1 "PingFang SC","Microsoft YaHei",sans-serif; box-shadow:0 12px 30px -10px rgba(29,78,216,.7); }
      .bar.open { display:block; }
      .bar { display:none; position:fixed; right:22px; bottom:158px; width:320px; background:#fff; border:1px solid #dfe7f2; border-radius:14px;
        box-shadow:0 18px 44px -18px rgba(15,35,70,.4); padding:12px 14px; font:13px/1.5 "PingFang SC","Microsoft YaHei",sans-serif; color:#16233b; }
      .hd { display:flex; align-items:center; gap:8px; font-weight:700; font-size:13px; }
      .hd .n { margin-left:auto; color:#2563eb; font-size:12px; }
      button { border:0; border-radius:9px; cursor:pointer; font:600 12.5px/1 inherit; font-family:inherit; }
      .toggle { width:100%; margin-top:10px; padding:9px 12px; background:#eff5ff; color:#1d4ed8; border:1px solid #d7e5ff; }
      .batch { width:100%; margin-top:8px; padding:10px 12px; background:linear-gradient(145deg,#3b82f6,#1d4ed8); color:#fff; display:none; }
      .batch:disabled { opacity:.5; cursor:not-allowed; }
      .status { margin-top:8px; font-size:11.5px; color:#6b7a90; min-height:17px; }
      .prog { height:6px; border-radius:999px; background:#eef2f8; overflow:hidden; margin-top:8px; }
      .prog i { display:block; height:100%; width:0; background:linear-gradient(90deg,#60a5fa,#2563eb); transition:width .3s; }
      .log { margin-top:7px; max-height:100px; overflow:auto; background:#0f172a; color:#dbeafe; border-radius:8px; padding:6px 8px;
        font:10.5px/1.6 Consolas,monospace; white-space:pre-wrap; word-break:break-all; }
      .foot { margin-top:7px; font-size:10.5px; color:#93a1b5; }
      .sm { display:block; margin-top:8px; font-size:10.5px; color:#6b7a90; }
      input { width:100%; box-sizing:border-box; margin-top:3px; padding:6px 8px; border:1px solid #d7e0ec; border-radius:8px;
        font:12px/1.3 Consolas,monospace; color:#16233b; background:#fbfdff; }
      .cfgrow { display:flex; gap:6px; margin-top:7px; }
      .cfgrow button { flex:1; padding:8px 6px; background:#eff5ff; color:#1d4ed8; border:1px solid #d7e5ff; }
    `;
    sh.appendChild(style);
    const fab = el("button", "album-fab", "推送到终端");
    fab.type = "button";
    fab.addEventListener("click", () => albumBar.classList.toggle("open"));
    sh.appendChild(fab);
    albumBar = el("div", "bar");
    albumBar.innerHTML = `
      <div class="hd">合集文章推送到终端<span class="n">已选 <b>0</b> 篇</span></div>
      <label class="sm">服务地址（终端 IP）</label>
      <input type="text" data-server spellcheck="false" placeholder="http://127.0.0.1:2178">
      <label class="sm">服务端下载目录（留空=服务端默认；Docker 填 /download）</label>
      <input type="text" data-outdir spellcheck="false" placeholder="留空使用服务端默认目录">
      <label class="sm">服务端重命名规则（留空=服务端默认）</label>
      <input type="text" data-tpl spellcheck="false" placeholder="留空使用服务端默认命名">
      <div class="cfgrow"><button data-test type="button">测试连接</button><button data-save type="button">保存设置</button></div>
      <button class="toggle" type="button">进入多选模式</button>
      <button class="batch" type="button" disabled>批量推送所选（0 篇）</button>
      <div class="prog"><i></i></div>
      <div class="status">默认关闭；进入后勾选文章，图片由服务端下载。</div>
      <div class="log"></div>
      <div class="foot">服务地址与“推送到终端”面板共用；与独立运行版脚本互不影响。</div>`;
    sh.appendChild(albumBar);
    const toggle = albumBar.querySelector(".toggle");
    serverInput = albumBar.querySelector("[data-server]");
    const outdirInput = albumBar.querySelector("[data-outdir]");
    const tplInput = albumBar.querySelector("[data-tpl]");
    serverInput.value = cfg.server;
    outdirInput.value = cfg.outDir;
    tplInput.value = cfg.template;
    albumBar.querySelector("[data-save]").addEventListener("click", () => {
      cfg.server = serverInput.value.trim() || DEFAULT_SERVER;
      cfg.outDir = outdirInput.value.trim();
      cfg.template = tplInput.value.trim();
      saveCfg(cfg);
      albumStatus.textContent = "已保存：" + cfg.server + " → " + cfg.outDir;
    });
    albumBar.querySelector("[data-test]").addEventListener("click", async () => {
      cfg.server = serverInput.value.trim() || DEFAULT_SERVER;
      saveCfg(cfg);
      try {
        const h = await api("GET", "/api/health");
        albumStatus.textContent = "连接正常：" + (h.app || "wxmp");
      } catch (e) { albumStatus.textContent = "连接失败：" + e.message; }
    });
    albumBtn = albumBar.querySelector(".batch");
    albumCount = albumBar.querySelector(".n b");
    albumStatus = albumBar.querySelector(".status");
    logEl = albumBar.querySelector(".log");
    barEl = albumBar.querySelector(".prog i");
    statusEl = albumStatus;
    toggle.addEventListener("click", () => setMulti(!multi));
    albumBtn.addEventListener("click", async () => {
      const sel = selected();
      if (!sel.length) return;
      const urls = sel.map(itemURL).filter(Boolean);
      try {
        const data = await api("POST", "/api/download", payload(urls));
        jobTimer = null;
        const job = data && data.job;
        if (!job) throw new Error("服务端没有返回任务");
        log("已提交 " + urls.length + " 篇 → 任务 " + job.id);
        pollJob(job.id);
      } catch (e) { log("✗ " + e.message, true); }
    });
    decorate();
    new MutationObserver(() => decorate()).observe(document.body, { childList: true, subtree: true });
  }

  if (isAlbumPage()) mountAlbumBar();
  else mount();
})();
