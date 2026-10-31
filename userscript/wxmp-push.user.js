// ==UserScript==
// @name         微信公众号图片下载器 · 推送终端版（wxmp push）
// @namespace    https://github.com/wxmp
// @version      1.0.3
// @description  把当前公众号文章/合集里选中的文章推送到本机（或 NAS）上运行的 wxmp 服务下载，图片落在服务端下载目录
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
        <div class="prog"><i data-bar></i></div>
        <div class="status" data-status>把当前文章推送到服务端下载</div>
        <button class="p" data-push type="button">推送当前文章</button>
        <div class="log" data-log></div>
        <div class="foot">脚本只发送文章链接，图片由服务端下载到它自己的下载目录；与“独立运行版”互不影响。</div>
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
    GM_registerMenuCommand("打开推送面板", togglePanel);
  }

  function togglePanel() {
    panel.classList.toggle("open");
    if (panel.classList.contains("open")) serverInput.focus();
  }

  function payload(urls) {
    const body = { urls };
    if (cfg.outDir) body.out_dir = cfg.outDir;
    if (cfg.template) body.name_template = cfg.template;
    return body;
  }

  async function pushURLs(urls) {
    if (busy) return;
    busy = true;
    pushBtn.disabled = true;
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
