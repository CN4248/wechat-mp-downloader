// ==UserScript==
// @name         微信公众号图片下载器（wxmp）
// @namespace    https://github.com/wxmp
// @version      1.2.3
// @description  一键下载微信公众号文章内的全部图片与动图原图，支持自定义命名规则、子文件夹与 ZIP 打包
// @author       wxmp
// @match        https://mp.weixin.qq.com/s/*
// @match        https://mp.weixin.qq.com/s?*
// @match        https://mp.weixin.qq.com/mp/appmsgalbum*
// @icon         data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCA2NCA2NCI+PHJlY3Qgd2lkdGg9IjY0IiBoZWlnaHQ9IjY0IiByeD0iMTQiIGZpbGw9IiMwN2MxNjAiLz48dGV4dCB4PSIzMiIgeT0iNDMiIGZvbnQtc2l6ZT0iMzAiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGZpbGw9IiNmZmYiPuW+rhwvdGV4dD48L3N2Zz4=
// @grant        GM_xmlhttpRequest
// @grant        GM_setValue
// @grant        GM_getValue
// @grant        GM_registerMenuCommand
// @connect      mmbiz.qpic.cn
// @connect      mmbiz.qlogo.cn
// @connect      *.qpic.cn
// @connect      *.qlogo.cn
// @connect      mp.weixin.qq.com
// @run-at       document-idle
// ==/UserScript==

(function () {
  "use strict";

  /* =========================================================
   * 纯函数区（可在 Node 中 require 做单元测试）
   * ======================================================= */

  const DEFAULT_TEMPLATE = "<title>-<post_id>-<order>.<ext>";
  const TOKENS = [
    ["<title>", "文章标题"], ["<post_id>", "文章短链 id"], ["<order>", "图片序号"],
    ["<ext>", "扩展名"], ["<account>", "公众号"], ["<author>", "作者"],
    ["<date>", "日期 YYYYMMDD"], ["<year>", "年"], ["<month>", "月"], ["<day>", "日"],
    ["<kind>", "类型 image/gif"], ["<width>", "宽度"], ["<height>", "高度"],
    ["<order2>", "两位序号"], ["<biz>", "biz"], ["<sn>", "sn"], ["<mid>", "mid"], ["<idx>", "idx"],
  ];
  const ALIASES = {
    "文章标题": "<title>", "文章id": "<post_id>", "文章ID": "<post_id>",
    "图片序号": "<order>", "扩展名": "<ext>", "公众号": "<account>", "作者": "<author>",
    "日期": "<date>", "年": "<year>", "月": "<month>", "日": "<day>",
    "类型": "<kind>", "宽度": "<width>", "高度": "<height>",
  };

  function decodeEntities(s) {
    return String(s == null ? "" : s)
      .replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">")
      .replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&apos;/g, "'")
      .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
      .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(parseInt(d, 10)))
      .replace(/\\x26/g, "&").replace(/\\\//g, "/");
  }

  function stripTags(s) {
    return decodeEntities(String(s || "").replace(/<[^>]*>/g, "")).trim();
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
      if (["png", "gif", "webp", "bmp", "svg"].includes(v)) return v;
    }
    const t = String(dataType || "").toLowerCase();
    if (t === "jpeg" || t === "jpg") return "jpg";
    if (["png", "gif", "webp", "bmp"].includes(t)) return t;
    for (const kw of ["mmbiz_gif", "mmbiz_png", "mmbiz_jpg", "mmbiz_webp"]) {
      if (low.includes(kw)) return kw.slice(6);
    }
    const m = low.match(/\.(png|jpe?g|gif|webp|bmp)(?:[?#]|$)/);
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

  function previewURL(raw, size) {
    let s = decodeEntities(String(raw || "").trim());
    if (s.startsWith("//")) s = "https:" + s;
    try {
      const u = new URL(s);
      const parts = u.pathname.split("/");
      if (parts.length && SIZE_SEG.test(parts[parts.length - 1])) parts[parts.length - 1] = String(size || 300);
      u.pathname = parts.join("/");
      return u.toString();
    } catch (_) {
      return s;
    }
  }

  function cdnKey(raw) {
    try {
      const u = new URL(decodeEntities(String(raw || "")));
      const parts = u.pathname.replace(/^\/+|\/+$/g, "").split("/");
      if (parts.length && /^\d{1,4}$/.test(parts[parts.length - 1])) parts.pop();
      const last = parts[parts.length - 1] || "";
      // 用完整文件名去重：不同图片可能共享前 33 个字符，截前缀会误合并；
      // 同一张图的正文/图集变体只是目录不同，文件名完全一致。
      if (last.length >= 24) return "h:" + last.toLowerCase();
      return (u.host || "").toLowerCase() + "/" + parts.join("/");
    } catch (_) {
      return String(raw || "");
    }
  }

  function grabVar(html, name) {
    const re = new RegExp("(?:var\\s+|window\\.)" + name + "\\s*=\\s*", "g");
    let m;
    while ((m = re.exec(html))) {
      const rest = html.slice(m.index + m[0].length);
      const v = readJSValue(rest);
      if (v != null && v !== "") return v;
    }
    return "";
  }

  function readJSValue(s) {
    s = s.replace(/^\s+/, "");
    if (s[0] === "'" || s[0] === '"') return readJSString(s);
    if (/^-?\d/.test(s)) return (s.match(/^-?\d+/) || [""])[0];
    let m = s.match(/^htmlDecode\(\s*(['"])/);
    if (m) return decodeEntities(readJSString(s.slice(m[0].length - 1)));
    return "";
  }

  function readJSString(s) {
    const q = s[0];
    let out = "";
    for (let i = 1; i < s.length; i++) {
      const c = s[i];
      if (c === "\\") {
        const n = s[i + 1];
        if (n === "n") out += "\n";
        else if (n === "t") out += "\t";
        else if (n === "r") out += "\r";
        else if (n === "u" && /^[0-9a-f]{4}$/i.test(s.slice(i + 2, i + 6))) { out += String.fromCharCode(parseInt(s.slice(i + 2, i + 6), 16)); i += 4; }
        else if (n === "x" && /^[0-9a-f]{2}$/i.test(s.slice(i + 2, i + 4))) { out += String.fromCharCode(parseInt(s.slice(i + 2, i + 4), 16)); i += 2; }
        else out += n || "";
        i++;
      } else if (c === q) {
        return out;
      } else {
        out += c;
      }
    }
    return out;
  }

  function contentSlice(html) {
    const i = html.indexOf('id="js_content"');
    if (i < 0) return "";
    const start = html.lastIndexOf("<div", i);
    if (start < 0) return "";
    // 先把 script/style/注释掩码成等长空格，避免脚本里的 </div> 干扰深度计数。
    const masked = html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, (x) => " ".repeat(x.length))
      .replace(/<style\b[^>]*>[\s\S]*?<\/style>/gi, (x) => " ".repeat(x.length))
      .replace(/<!--[\s\S]*?-->/g, (x) => " ".repeat(x.length));
    const tagRe = /<div\b|<\/div>/gi;
    tagRe.lastIndex = start;
    let depth = 0, m;
    while ((m = tagRe.exec(masked))) {
      if (m[0].toLowerCase().startsWith("<div")) depth++; else depth--;
      if (depth === 0) return html.slice(start, m.index + m[0].length);
    }
    return html.slice(start);
  }

  function attrsOf(tag) {
    const out = {};
    const re = /([\w:-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))/g;
    let m;
    while ((m = re.exec(tag))) out[m[1].toLowerCase()] = decodeEntities(m[3] != null ? m[3] : m[4] != null ? m[4] : m[5]);
    return out;
  }

  function parsePictureList(html) {
    const i = html.indexOf("picture_page_info_list");
    if (i < 0) return [];
    const b = html.indexOf("[", i);
    if (b < 0) return [];
    let depth = 0, end = -1;
    for (let p = b; p < html.length; p++) {
      if (html[p] === "[") depth++;
      else if (html[p] === "]") { depth--; if (depth === 0) { end = p; break; } }
    }
    if (end < 0) return [];
    const arr = html.slice(b + 1, end);
    const out = [];
    // 只取数组元素（花括号深度 1）里的顶层 cdn_url，跳过 watermark_info 等嵌套对象。
    let brace = 0, objStart = -1;
    for (let p = 0; p < arr.length; p++) {
      const ch = arr[p];
      if (ch === "{") {
        if (brace === 0) objStart = p;
        brace++;
      } else if (ch === "}") {
        brace--;
        if (brace === 0 && objStart >= 0) {
          const obj = arr.slice(objStart, p + 1);
          const u = obj.match(/cdn_url\s*:\s*(['"])([^'"]+)\1/);
          if (u) {
            const w = (obj.match(/width\s*:\s*['"]?(\d+)/) || [])[1];
            const h = (obj.match(/height\s*:\s*['"]?(\d+)/) || [])[1];
            out.push({ url: decodeEntities(u[2]), width: Number(w) || 0, height: Number(h) || 0 });
          }
          objStart = -1;
        }
      }
    }
    return out;
  }

  function parsePageHTML(html, pageURL) {
    const page = String(html || "");
    const art = {
      url: pageURL || "",
      title: stripTags(grabVar(page, "msg_title")) || stripTags((page.match(/<meta[^>]+property=["']og:title["'][^>]*>/i) || [""])[0].replace(/^.*content=["']([^"']*)["'].*$/i, "$1")) || stripTags((page.match(/<title[^>]*>([\s\S]*?)<\/title>/i) || ["", ""])[1]),
      account: stripTags(grabVar(page, "nickname")),
      author: stripTags(grabVar(page, "author")),
      post_id: ((pageURL || "").match(/\/s\/([A-Za-z0-9_-]{6,})/) || [])[1] || grabVar(page, "appmsgid") || grabVar(page, "mid") || "",
      biz: grabVar(page, "biz"),
      sn: grabVar(page, "sn"),
      mid: grabVar(page, "mid") || grabVar(page, "appmsgid"),
      idx: grabVar(page, "idx"),
      publish_time: Number(grabVar(page, "ct")) || 0,
      cover: decodeEntities(grabVar(page, "msg_cdn_url")),
      medias: [],
      warnings: [],
    };
    if (!art.title) art.title = "未命名文章";

    const content = contentSlice(page) || page;
    const seen = new Set();
    const contentDims = [];
    const re = /<img\b[^>]*>/gi;
    let m;
    while ((m = re.exec(content))) {
      const a = attrsOf(m[0]);
      const raw = a["data-src"] || a.src || "";
      if (!raw || /^data:/i.test(raw)) continue;
      if (!/qpic\.cn|qlogo\.cn|qpic\.cn\/|mmbiz/i.test(raw) && !/wx_fmt=/.test(raw)) continue;
      const type = (a["data-type"] || "").toLowerCase();
      let ext = extFromURL(raw, type);
      const isGIF = type === "gif" || ext === "gif" || /mmbiz_gif/i.test(raw);
      if (isGIF) ext = "gif";
      const w = Number(a["data-w"]) || Number(a.width) || 0;
      const ratio = Number(a["data-ratio"]) || 0;
      const h = Number(a.height) || (w > 0 && ratio > 0 ? Math.round(w * ratio) : 0);
      const orig = originalURL(raw, ext);
      if (!orig) continue;
      const key = cdnKey(orig);
      if (seen.has(key)) continue;
      seen.add(key);
      art.medias.push({
        seq: art.medias.length + 1,
        kind: isGIF ? "gif" : "image",
        url: orig,
        preview_url: previewURL(raw, 300),
        ext,
        width: w,
        height: h,
        is_gif: isGIF,
        file_id: a["data-imgfileid"] || "",
        source: "content",
      });
      if (w > 0 && h > 0) contentDims.push([w, h]);
    }

    const pics = parsePictureList(page);
    const doubleMirror = contentDims.length > 0 && pics.length === 2 * contentDims.length;
    const mirror = pics.length > 0 && (pics.length === contentDims.length || doubleMirror);
    let mirrorSkipped = 0;
    for (let pi = 0; pi < pics.length; pi++) {
      const p = pics[pi];
      const mi = doubleMirror ? pi % contentDims.length : pi;
      const ext = extFromURL(p.url, "");
      const orig = originalURL(p.url, ext);
      if (!orig) continue;
      const key = cdnKey(orig);
      if (mirror && contentDims[mi] && p.width === contentDims[mi][0] && p.height === contentDims[mi][1]) {
        if (!seen.has(key)) mirrorSkipped++;
        continue;
      }
      if (seen.has(key)) continue;
      seen.add(key);
      art.medias.push({
        seq: art.medias.length + 1,
        kind: ext === "gif" ? "gif" : "image",
        url: orig,
        preview_url: previewURL(p.url, 300),
        ext,
        width: p.width,
        height: p.height,
        is_gif: ext === "gif",
        file_id: "",
        source: "picture",
      });
    }
    if (mirrorSkipped > 0) art.warnings.push(`图集与正文重复，已忽略 ${mirrorSkipped} 张`);

    art.medias.forEach((x, i) => { x.seq = i + 1; });
    if (!art.medias.length) art.warnings.push("没有在页面中找到图片");
    return art;
  }

  /* ---------- naming ---------- */

  function pad(n, len) {
    let s = String(n);
    while (s.length < (len || 2)) s = "0" + s;
    return s;
  }

  function dateParts(ts) {
    const d = ts > 0 ? new Date(ts * 1000) : new Date();
    return {
      date: `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}`,
      year: String(d.getFullYear()),
      month: pad(d.getMonth() + 1),
      day: pad(d.getDate()),
    };
  }

  function canonical(token) {
    if (ALIASES[token]) return ALIASES[token];
    const t = "<" + String(token).replace(/^<|>$/g, "") + ">";
    return TOKENS.some((x) => x[0] === t) ? t : "";
  }

  function validateTemplate(tpl) {
    const s = String(tpl || "").trim();
    if (!s) throw new Error("命名规则不能为空");
    let found = false;
    const re = /<([^<>]+)>/g;
    let m;
    while ((m = re.exec(s))) {
      found = true;
      if (!canonical(m[1])) throw new Error("未知变量 <" + m[1] + ">");
    }
    if (!found) throw new Error("命名规则里至少需要一个变量");
    return true;
  }

  function tokenValue(media, token) {
    const d = dateParts(media.publish_time);
    switch (token) {
      case "<title>": return media.title || "未命名文章";
      case "<post_id>": return media.post_id || "";
      case "<order>": return String(media.seq || 1);
      case "<order2>": return pad(media.seq || 1);
      case "<ext>": return normalizeExt(media.ext);
      case "<account>": return media.account || "";
      case "<author>": return media.author || "";
      case "<date>": return d.date;
      case "<year>": return d.year;
      case "<month>": return d.month;
      case "<day>": return d.day;
      case "<kind>": return media.kind || (media.is_gif ? "gif" : "image");
      case "<width>": return String(media.width || 0);
      case "<height>": return String(media.height || 0);
      case "<biz>": return media.biz || "";
      case "<sn>": return media.sn || "";
      case "<mid>": return media.mid || "";
      case "<idx>": return media.idx || "";
      default: return "";
    }
  }

  function sanitizeSegment(seg) {
    let s = String(seg || "")
      .replace(/[<>:"|?*\u0000-\u001f]/g, "_")
      .replace(/\s+/g, " ")
      .trim()
      .replace(/[. ]+$/, "");
    if (s === "." || s === ".." || s === "") s = "_";
    if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(s)) s = "_" + s;
    if (s.length > 100) s = s.slice(0, 100);
    return s;
  }

  function hasExtToken(tpl) {
    const re = /<([^<>]+)>/g;
    let m;
    while ((m = re.exec(tpl))) if (canonical(m[1]) === "<ext>") return true;
    return false;
  }

  function buildRelPath(media, tpl) {
    const template = String(tpl || DEFAULT_TEMPLATE).trim() || DEFAULT_TEMPLATE;
    const ext = normalizeExt(media.ext);
    const segs = template.split(/[\/\\]+/).filter((x) => x !== "");
    const names = segs.map((raw) => {
      let s = raw.replace(/<([^<>]+)>/g, (_, name) => {
        const c = canonical(name);
        return c ? tokenValue(media, c) : "";
      });
      s = s.replace(/[\\/:*?"<>|\u0000-\u001f]/g, "_").trim();
      return s;
    }).filter((x) => x !== "");
    if (!names.length) names.push(sanitizeSegment(media.title || "未命名"));
    if (!hasExtToken(template)) names[names.length - 1] += "." + ext;
    return names.map((x, i) => (i === names.length - 1 ? x.replace(/[<>:"|?*]/g, "_") : sanitizeSegment(x))).join("/");
  }

  /* ---------- zip (store) ---------- */

  const CRC_TABLE = (() => {
    const t = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      t[n] = c >>> 0;
    }
    return t;
  })();

  function crc32(bytes) {
    let c = 0xffffffff;
    for (let i = 0; i < bytes.length; i++) c = CRC_TABLE[(c ^ bytes[i]) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  }

  function dosDateTime(d) {
    const time = (d.getHours() << 11) | (d.getMinutes() << 5) | Math.floor(d.getSeconds() / 2);
    const date = ((d.getFullYear() - 1980) << 9) | ((d.getMonth() + 1) << 5) | d.getDate();
    return { time, date };
  }

  function buildZip(files) {
    const enc = new TextEncoder();
    const chunks = [];
    const central = [];
    let offset = 0;
    const { time, date } = dosDateTime(new Date());

    for (const f of files) {
      const nameBytes = enc.encode(f.name);
      const data = f.data instanceof Uint8Array ? f.data : new Uint8Array(f.data);
      const crc = crc32(data);
      const local = new Uint8Array(30 + nameBytes.length);
      const lv = new DataView(local.buffer);
      lv.setUint32(0, 0x04034b50, true);
      lv.setUint16(4, 20, true);
      lv.setUint16(6, 0x0800, true);
      lv.setUint16(8, 0, true);
      lv.setUint16(10, time, true);
      lv.setUint16(12, date, true);
      lv.setUint32(14, crc, true);
      lv.setUint32(18, data.length, true);
      lv.setUint32(22, data.length, true);
      lv.setUint16(26, nameBytes.length, true);
      lv.setUint16(28, 0, true);
      local.set(nameBytes, 30);
      chunks.push(local, data);

      const cd = new Uint8Array(46 + nameBytes.length);
      const cv = new DataView(cd.buffer);
      cv.setUint32(0, 0x02014b50, true);
      cv.setUint16(4, 20, true);
      cv.setUint16(6, 20, true);
      cv.setUint16(8, 0x0800, true);
      cv.setUint16(10, 0, true);
      cv.setUint16(12, time, true);
      cv.setUint16(14, date, true);
      cv.setUint32(16, crc, true);
      cv.setUint32(20, data.length, true);
      cv.setUint32(24, data.length, true);
      cv.setUint16(28, nameBytes.length, true);
      cv.setUint16(30, 0, true);
      cv.setUint16(32, 0, true);
      cv.setUint16(34, 0, true);
      cv.setUint16(36, 0, true);
      cv.setUint32(38, 0, true);
      cv.setUint32(42, offset, true);
      cd.set(nameBytes, 46);
      central.push(cd);

      offset += local.length + data.length;
    }

    let cdSize = 0;
    for (const c of central) cdSize += c.length;
    const eocd = new Uint8Array(22);
    const ev = new DataView(eocd.buffer);
    ev.setUint32(0, 0x06054b50, true);
    ev.setUint16(4, 0, true);
    ev.setUint16(6, 0, true);
    ev.setUint16(8, files.length, true);
    ev.setUint16(10, files.length, true);
    ev.setUint32(12, cdSize, true);
    ev.setUint32(16, offset, true);
    ev.setUint16(20, 0, true);

    return new Blob([...chunks, ...central, eocd], { type: "application/zip" });
  }

  /* =========================================================
   * 运行区（浏览器 / Tampermonkey）
   * ======================================================= */

  if (typeof document === "undefined" || typeof GM_info === "undefined") {
    if (typeof module !== "undefined" && module.exports) {
      module.exports = {
        DEFAULT_TEMPLATE, TOKENS, decodeEntities, parsePageHTML, contentSlice,
        originalURL, previewURL, cdnKey, extFromURL, buildRelPath, validateTemplate, parsePictureList,
        sanitizeSegment, buildZip, crc32, dateParts,
      };
    }
    return;
  }

  const CFG_KEY = "wxmp.cfg.v1";

  function loadCfg() {
    try { return JSON.parse(GM_getValue(CFG_KEY, "{}")); } catch (_) { return {}; }
  }
  function saveCfg(cfg) { try { GM_setValue(CFG_KEY, JSON.stringify(cfg)); } catch (_) {} }

  const cfg = Object.assign({
    template: DEFAULT_TEMPLATE,
    zip: true,
    cover: false,
  }, loadCfg());

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  let shadow, fab, panel, logBox, bar, statusText, btnStart, countBadge;
  let busy = false;
  let cancelFlag = false;

  function mount() {
    const host = el("div");
    host.id = "wxmp-host";
    host.style.cssText = "position:fixed;z-index:2147483000;right:0;bottom:0;width:0;height:0;";
    document.documentElement.appendChild(host);
    shadow = host.attachShadow({ mode: "open" });

    const style = el("style");
    style.textContent = `
      :host { all: initial; }
      * { box-sizing: border-box; }
      .fab {
        position: fixed; right: 22px; bottom: 22px; display: flex; align-items: center; gap: 8px;
        padding: 11px 17px; border-radius: 999px; border: 0; cursor: pointer;
        background: linear-gradient(145deg,#12cc6b,#05a04c); color: #fff;
        font: 600 13px/1 "PingFang SC","Microsoft YaHei",sans-serif;
        box-shadow: 0 12px 30px -10px rgba(6,150,75,.75);
      }
      .fab:hover { transform: translateY(-1px); }
      .fab .b { background: rgba(255,255,255,.22); border-radius: 999px; padding: 2px 7px; font-size: 11px; }
      .panel {
        position: fixed; right: 22px; bottom: 84px; width: 372px; max-height: 78vh; overflow: auto;
        background: #fff; border: 1px solid #e2ebe6; border-radius: 16px;
        box-shadow: 0 24px 60px -20px rgba(10,45,30,.5);
        font: 13px/1.55 "PingFang SC","Microsoft YaHei",sans-serif; color: #16302b;
        display: none;
      }
      .panel.open { display: block; }
      .hd { display: flex; align-items: center; gap: 8px; padding: 13px 15px; border-bottom: 1px solid #eef4f0; }
      .hd .logo { width: 26px; height: 26px; border-radius: 8px; background: linear-gradient(145deg,#21d06f,#05a04c); color:#fff; display:grid; place-items:center; font-size:13px; font-weight:700;}
      .hd b { font-size: 13.5px; }
      .hd .x { margin-left: auto; border: 0; background: #f2f6f3; width: 26px; height: 26px; border-radius: 50%; cursor: pointer; color:#4a615b; font-size: 15px; line-height:1;}
      .bd { padding: 13px 15px 15px; }
      .title { font-weight: 700; font-size: 13px; margin-bottom: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      .meta { color: #7d918c; font-size: 11.5px; margin-bottom: 10px; }
      .row { display: flex; align-items: center; gap: 7px; }
      .grow { flex: 1; }
      label.sm { display:block; font-size:11.5px; color:#4a615b; font-weight:600; margin:9px 0 4px; }
      input[type=text] { width:100%; border:1px solid #e2ebe6; border-radius:8px; padding:7px 9px; font:12px/1.4 "JetBrains Mono",Consolas,monospace; color:#16302b; outline:none; }
      input[type=text]:focus { border-color:#8ee0b2; box-shadow:0 0 0 3px rgba(7,193,96,.12); }
      .chips { display:flex; flex-wrap:wrap; gap:4px; margin-top:6px; }
      .chip { border:1px solid #d9ece2; background:#f5fbf7; color:#059a4c; border-radius:999px; padding:2px 7px; font:10.5px/1.5 "JetBrains Mono",Consolas,monospace; cursor:pointer; }
      .chk { display:flex; align-items:center; gap:5px; color:#4a615b; font-size:12px; }
      .chk input { accent-color:#07c160; }
      .btns { display:flex; gap:8px; margin-top:12px; }
      button.p { flex:1; border:0; border-radius:9px; padding:9px 12px; background:linear-gradient(145deg,#12cc6b,#05a04c); color:#fff; font:600 12.5px/1 inherit; font-family:inherit; cursor:pointer; }
      button.p:disabled { opacity:.55; cursor:not-allowed; }
      button.g { border:1px solid #e2ebe6; background:#fff; border-radius:9px; padding:9px 12px; color:#4a615b; font:600 12px/1 inherit; font-family:inherit; cursor:pointer; }
      .prog { height:7px; border-radius:999px; background:#edf4ef; overflow:hidden; margin-top:12px; }
      .prog i { display:block; height:100%; width:0; background:linear-gradient(90deg,#23d478,#06a851); transition:width .3s; }
      .status { font-size:11.5px; color:#7d918c; margin-top:6px; min-height:17px; }
      .log { margin-top:8px; max-height:130px; overflow:auto; background:#10201b; color:#cfe9d9; border-radius:9px; padding:8px 10px; font:10.5px/1.7 "JetBrains Mono",Consolas,monospace; white-space:pre-wrap; word-break:break-all; }
      .log .err { color:#ffb3b3; }
      .warn { background:#fff6e5; color:#b26a00; border:1px solid #f6e2bd; border-radius:8px; padding:7px 9px; font-size:11.5px; margin-top:8px; white-space:pre-wrap; }
      .foot { font-size:10.5px; color:#9aada7; margin-top:10px; }
    `;
    shadow.appendChild(style);

    fab = el("button", "fab");
    fab.type = "button";
    fab.innerHTML = "下载本文图片 <span class='b'>0</span>";
    countBadge = fab.querySelector(".b");
    fab.addEventListener("click", togglePanel);
    shadow.appendChild(fab);

    panel = el("div", "panel");
    panel.innerHTML = `
      <div class="hd"><span class="logo">微</span><b>公众号图片下载器</b><button class="x" type="button" title="关闭">×</button></div>
      <div class="bd">
        <div class="title" data-t>正在读取文章…</div>
        <div class="meta" data-m></div>
        <label class="sm">命名规则（/ 可创建子文件夹）</label>
        <input type="text" data-tpl spellcheck="false">
        <div class="chips" data-chips></div>
        <label class="sm">选项</label>
        <div class="row" style="flex-wrap:wrap;gap:10px">
          <label class="chk"><input type="checkbox" data-zip> 打包为 ZIP</label>
          <label class="chk"><input type="checkbox" data-cover> 包含封面</label>
          <button class="g" data-dir type="button" style="padding:5px 9px">选择文件夹</button>
        </div>
        <div class="btns"><button class="p" data-start type="button">开始下载</button><button class="g" data-reset type="button">默认规则</button></div>
        <div class="prog"><i data-bar></i></div>
        <div class="status" data-status>等待开始</div>
        <div class="log" data-log></div>
        <div class="warn" data-warn style="display:none"></div>
        <div class="foot">全部在浏览器本地解析、取图、打包；不连接任何本地服务，也不需要电脑端程序。支持“选择文件夹”直接落盘（保留子文件夹）。</div>
      </div>`;
    shadow.appendChild(panel);

    const $ = (s) => panel.querySelector(s);
    logBox = $("[data-log]");
    bar = $("[data-bar]");
    statusText = $("[data-status]");
    btnStart = $("[data-start]");

    const tplInput = $("[data-tpl]");
    tplInput.value = cfg.template;
    $("[data-zip]").checked = !!cfg.zip;
    $("[data-cover]").checked = !!cfg.cover;

    tplInput.addEventListener("input", () => { cfg.template = tplInput.value; saveCfg(cfg); });
    $("[data-zip]").addEventListener("change", (e) => { cfg.zip = e.target.checked; saveCfg(cfg); });
    $("[data-cover]").addEventListener("change", (e) => { cfg.cover = e.target.checked; saveCfg(cfg); });
    $("[data-reset]").addEventListener("click", () => { tplInput.value = DEFAULT_TEMPLATE; cfg.template = DEFAULT_TEMPLATE; saveCfg(cfg); });
    panel.querySelector(".x").addEventListener("click", closePanel);
    btnStart.addEventListener("click", startDownload);
    $("[data-dir]").addEventListener("click", chooseDir);

    const chips = $("[data-chips]");
    TOKENS.slice(0, 8).forEach(([t, d]) => {
      const c = el("button", "chip", t);
      c.type = "button";
      c.title = d;
      c.addEventListener("click", () => {
        const s = tplInput.selectionStart ?? tplInput.value.length;
        tplInput.value = tplInput.value.slice(0, s) + t + tplInput.value.slice(s);
        cfg.template = tplInput.value;
        saveCfg(cfg);
        tplInput.focus();
      });
      chips.appendChild(c);
    });

    GM_registerMenuCommand("打开图片下载面板", togglePanel);
    updateFabCount();
    window.addEventListener("resize", () => {});
  }

  function togglePanel() {
    if (panel.classList.contains("open")) { closePanel(); return; }
    panel.classList.add("open");
    parseCurrent();
  }

  function closePanel() { panel.classList.remove("open"); }

  let article = null;

  function parseCurrent() {
    try {
      article = parsePageHTML(document.documentElement.outerHTML, location.href);
      const $ = (s) => panel.querySelector(s);
      $("[data-t]").textContent = article.title;
      $("[data-m]").textContent = [article.account, article.post_id ? "id " + article.post_id : "", article.medias.length + " 项"].filter(Boolean).join(" · ");
      const warn = $("[data-warn]");
      if (article.warnings.length) { warn.style.display = "block"; warn.textContent = article.warnings.join("\n"); }
      else warn.style.display = "none";
      updateFabCount();
      statusText.textContent = article.medias.length ? "可以开始下载" : "未找到可下载的图片";
    } catch (e) {
      log("解析失败：" + e.message, true);
    }
  }

  function updateFabCount() {
    if (countBadge) countBadge.textContent = article ? String(article.medias.length) : "0";
  }

  function log(msg, isErr) {
    if (!logBox) return;
    const line = el("div", isErr ? "err" : "", msg);
    logBox.appendChild(line);
    logBox.scrollTop = logBox.scrollHeight;
  }

  function setProgress(done, total) {
    if (bar) bar.style.width = (total ? Math.round((done / total) * 100) : 0) + "%";
    if (statusText) statusText.textContent = `进度 ${done}/${total}`;
  }

  function fetchBinary(url) {
    // 完全在浏览器内取图：Tampermonkey 用 GM_xmlhttpRequest 绕过跨域；
    // 没有该 API 时退回 fetch（部分浏览器/管理器会因 CORS 失败，会给出明确错误）。
    if (typeof GM_xmlhttpRequest === "function") {
      return new Promise((resolve, reject) => {
        GM_xmlhttpRequest({
          method: "GET",
          url,
          responseType: "arraybuffer",
          timeout: 60000,
          headers: { Referer: "https://mp.weixin.qq.com/", "User-Agent": navigator.userAgent },
          onload: (r) => {
            if (r.status >= 200 && r.status < 300) {
              resolve(new Uint8Array(r.response));
            } else {
              reject(new Error("HTTP " + r.status));
            }
          },
          onerror: () => reject(new Error("网络错误")),
          ontimeout: () => reject(new Error("下载超时")),
        });
      });
    }
    return fetch(url, { credentials: "omit", referrer: "https://mp.weixin.qq.com/" })
      .then((r) => {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.arrayBuffer();
      })
      .then((buf) => new Uint8Array(buf));
  }

  async function fetchWithRetry(url, tries) {
    let err;
    for (let i = 1; i <= (tries || 3); i++) {
      try { return await fetchBinary(url); }
      catch (e) { err = e; await new Promise((r) => setTimeout(r, 500 * i)); }
    }
    throw err;
  }

  async function pool(items, limit, worker, onDone) {
    let next = 0, done = 0;
    async function run() {
      while (next < items.length) {
        if (cancelFlag) return;
        const i = next++;
        await worker(items[i], i);
        done++;
        onDone(done);
      }
    }
    const runners = [];
    for (let i = 0; i < Math.min(limit, items.length); i++) runners.push(run());
    await Promise.all(runners);
  }

  let dirHandle = null;

  async function writeToDir(root, relPath, blob) {
    const parts = String(relPath).split("/").filter((x) => x && x !== "." && x !== "..");
    let dir = root;
    for (let i = 0; i < parts.length - 1; i++) {
      dir = await dir.getDirectoryHandle(sanitizeSegment(parts[i]), { create: true });
    }
    const fh = await dir.getFileHandle(sanitizeSegment(parts[parts.length - 1]), { create: true });
    const w = await fh.createWritable();
    await w.write(blob);
    await w.close();
    return relPath;
  }

  // 选择文件夹（File System Access API）：直接把图写进去，保留子文件夹，不弹下载框。
  async function chooseDir() {
    if (typeof window.showDirectoryPicker !== "function") {
      log("当前浏览器不支持“选择文件夹”，将使用浏览器下载（ZIP 模式最省事）", true);
      return;
    }
    try {
      dirHandle = await window.showDirectoryPicker({ mode: "readwrite" });
      log("已选择保存文件夹：" + (dirHandle.name || "(未命名)"));
      // 选了文件夹就默认逐张写入，避免只得到一个 ZIP。
      if (cfg.zip) {
        cfg.zip = false;
        saveCfg(cfg);
        const z = panel && panel.querySelector("[data-zip]");
        if (z) z.checked = false;
      }
      statusText.textContent = "将直接写入所选文件夹（保留子文件夹）";
      const dt = albumBar && albumBar.querySelector("[data-dirtext]");
      if (dt) dt.textContent = "已选：" + (dirHandle.name || "(未命名)");
    } catch (e) {
      if (e && e.name !== "AbortError") log("选择文件夹失败：" + e.message, true);
    }
  }

  async function sha256Hex(buf) {
    if (typeof crypto === "undefined" || !crypto.subtle) return "";
    const h = await crypto.subtle.digest("SHA-256", buf);
    return Array.from(new Uint8Array(h)).map((b) => b.toString(16).padStart(2, "0")).join("");
  }

  async function saveBlob(blob, filename) {
    if (dirHandle) {
      try {
        await writeToDir(dirHandle, filename, blob);
        return;
      } catch (e) {
        log("写入文件夹失败，改用浏览器下载：" + e.message, true);
        dirHandle = null;
      }
    }
    // 原生 Blob 下载：不依赖 GM_download，任何现代浏览器都能用。
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename.replace(/\//g, "_");
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 60000);
  }

  // 下载一篇已经解析好的文章；返回统计，供单篇与合集批量复用。
  async function downloadArticle(art, opts) {
    opts = opts || {};
    const zip = opts.zip !== undefined ? !!opts.zip : !!cfg.zip;
    const cover = opts.cover !== undefined ? !!opts.cover : !!cfg.cover;
    try { validateTemplate(cfg.template); }
    catch (e) { log(e.message, true); return { total: 0, ok: 0, failed: 0, duplicate: 0, error: e.message }; }

    const medias = (art.medias || []).slice();
    if (cover && art.cover) {
      medias.unshift({
        seq: 0, kind: "image", url: originalURL(art.cover, "jpg"),
        preview_url: previewURL(art.cover, 300), ext: extFromURL(art.cover, "jpeg"),
        width: 0, height: 0, is_gif: false, source: "cover",
        title: art.title, post_id: art.post_id, account: art.account,
        author: art.author, biz: art.biz, sn: art.sn, mid: art.mid,
        idx: art.idx, publish_time: art.publish_time,
      });
      medias.forEach((m, i) => { if (m.source !== "cover") m.seq = i; });
      if (!medias[0].url) medias.shift();
    }
    if (!medias.length) return { total: 0, ok: 0, failed: 0, duplicate: 0, error: "没有可下载的图片" };

    const files = [];
    let failed = 0, duplicated = 0;
    const seenBytes = new Set();
    const total = medias.length;
    log(`开始下载 ${art.title || "文章"}（${total} 项）…`);

    await pool(medias, 3, async (m) => {
      const rel = buildRelPath(Object.assign({}, art, m), cfg.template);
      try {
        const data = await fetchWithRetry(m.url);
        if (data.length === 0) throw new Error("空文件");
        const digest = await sha256Hex(data);
        if (digest && seenBytes.has(digest)) {
          duplicated++;
          log("= " + rel + "  与已有图片内容相同，跳过");
          return;
        }
        if (digest) seenBytes.add(digest);
        if (zip) {
          files.push({ name: rel, data });
        } else {
          await saveBlob(new Blob([data], { type: "image/" + normalizeExt(m.ext) }), rel);
        }
        log("✓ " + rel + "  " + (data.length / 1024).toFixed(0) + " KB");
      } catch (e) {
        failed++;
        log("✗ " + rel + "  " + e.message, true);
      }
    }, (done) => setProgress(done, total));

    let zipName = "";
    if (zip && files.length) {
      files.sort((a, b) => a.name.localeCompare(b.name, "zh-CN"));
      zipName = sanitizeSegment(art.title || "公众号图片") + ".zip";
      log("打包 " + files.length + " 个文件…");
      const blob = buildZip(files);
      await saveBlob(blob, zipName);
      log("✓ 已生成 " + zipName + "（" + (blob.size / 1024 / 1024).toFixed(2) + " MB）");
    }
    return { total, ok: total - failed - duplicated, failed, duplicate: duplicated, zipName };
  }

  async function startDownload() {
    if (busy) return;
    if (!article) parseCurrent();
    if (!article || !article.medias.length) { log("没有可下载的图片", true); return; }
    try { validateTemplate(cfg.template); }
    catch (e) { log(e.message, true); statusText.textContent = e.message; return; }

    busy = true;
    cancelFlag = false;
    btnStart.disabled = true;
    logBox.innerHTML = "";
    try {
      const r = await downloadArticle(article, {});
      if (!r.total) { statusText.textContent = r.error || "没有可下载的图片"; return; }
      statusText.textContent = `完成：${r.ok} 张成功，${r.duplicate} 张重复跳过，${r.failed} 张失败`;
    } finally {
      busy = false;
      btnStart.disabled = false;
    }
  }

  /* =========================================================
   * 合集页多选（默认关闭，点击按钮才进入）
   * ======================================================= */

  function isAlbumPage() { return /\/mp\/appmsgalbum/.test(location.pathname); }
  function albumItems() { return Array.from(document.querySelectorAll("li.js_album_item")); }

  function albumItemURL(li) {
    const raw = li.getAttribute("data-link") || (li.querySelector("a[href]") || {}).href || "";
    try {
      const u = new URL(decodeEntities(raw), location.href);
      u.hash = "";
      return u.toString();
    } catch (_) { return ""; }
  }

  function fetchText(url) {
    if (typeof GM_xmlhttpRequest === "function") {
      return new Promise((resolve, reject) => {
        GM_xmlhttpRequest({
          method: "GET", url, timeout: 60000,
          headers: { Referer: "https://mp.weixin.qq.com/", "User-Agent": navigator.userAgent },
          onload: (r) => (r.status >= 200 && r.status < 300 ? resolve(r.responseText) : reject(new Error("HTTP " + r.status))),
          onerror: () => reject(new Error("网络错误")),
          ontimeout: () => reject(new Error("读取超时")),
        });
      });
    }
    return fetch(url, { credentials: "omit", referrer: "https://mp.weixin.qq.com/" }).then((r) => {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.text();
    });
  }

  let albumBar = null, albumStatus = null, albumLog = null, albumCount = null;
  let albumToggle = null, albumBatch = null;
  let multiMode = false;

  function albumSelected() {
    return albumItems().filter((li) => {
      const cb = li.querySelector(".wxmp-pick");
      return cb && cb.checked;
    });
  }

  function albumSyncCount() {
    if (!albumCount) return;
    albumCount.textContent = String(albumSelected().length);
    if (albumBatch) albumBatch.disabled = busy || albumSelected().length === 0;
  }

  function albumLogLine(msg, err) {
    if (!albumLog) return;
    const d = el("div", "", msg);
    if (err) d.style.color = "#ff9b9b";
    albumLog.appendChild(d);
    albumLog.scrollTop = albumLog.scrollHeight;
  }

  function decorateAlbumItems() {
    albumItems().forEach((li) => {
      if (li.querySelector(".wxmp-pickwrap")) return;
      li.style.position = "relative";
      const wrap = el("label", "wxmp-pickwrap");
      wrap.style.cssText = "position:absolute;top:8px;right:8px;z-index:20;display:" + (multiMode ? "flex" : "none") +
        ";align-items:center;justify-content:center;width:26px;height:26px;border-radius:50%;background:rgba(255,255,255,.94);border:1px solid #07c160;cursor:pointer;";
      const cb = el("input", "wxmp-pick");
      cb.type = "checkbox";
      cb.style.cssText = "width:16px;height:16px;accent-color:#07c160;cursor:pointer;margin:0;";
      cb.addEventListener("change", () => {
        wrap.style.background = cb.checked ? "#07c160" : "rgba(255,255,255,.94)";
        cb.style.accentColor = "#fff";
        li.style.outline = cb.checked ? "2px solid #07c160" : "";
        li.style.outlineOffset = "-2px";
        albumSyncCount();
      });
      wrap.appendChild(cb);
      li.appendChild(wrap);
      li.addEventListener("click", (e) => {
        if (!multiMode) return;
        if (e.target.closest(".wxmp-pickwrap")) return;
        e.preventDefault();
        e.stopPropagation();
        cb.checked = !cb.checked;
        cb.dispatchEvent(new Event("change"));
      }, true);
    });
    albumSyncCount();
  }

  function setMultiMode(on) {
    multiMode = !!on;
    albumItems().forEach((li) => {
      const wrap = li.querySelector(".wxmp-pickwrap");
      if (wrap) wrap.style.display = multiMode ? "flex" : "none";
      if (!multiMode) {
        const cb = li.querySelector(".wxmp-pick");
        if (cb) { cb.checked = false; cb.dispatchEvent(new Event("change")); }
      }
    });
    if (albumToggle) albumToggle.textContent = multiMode ? "退出多选模式" : "进入多选模式";
    if (albumBatch) albumBatch.style.display = multiMode ? "block" : "none";
    decorateAlbumItems();
    albumSyncCount();
  }

  async function albumBatchDownload() {
    if (busy) return;
    const sel = albumSelected();
    if (!sel.length) { albumStatus.textContent = "请先勾选要下载的文章"; return; }
    busy = true;
    cancelFlag = false;
    albumBatch.disabled = true;
    albumToggle.disabled = true;
    albumStatus.textContent = "准备下载 " + sel.length + " 篇…";
    let articles = 0, ok = 0, failed = 0;
    try {
      for (let i = 0; i < sel.length; i++) {
        if (cancelFlag) break;
        const url = albumItemURL(sel[i]);
        const title = sel[i].getAttribute("data-title") || ("第 " + (i + 1) + " 篇");
        albumStatus.textContent = `[${i + 1}/${sel.length}] ${title}`;
        try {
          const html = await fetchText(url);
          const art = parsePageHTML(html, url);
          if (!art.medias.length) throw new Error("没有解析到图片");
          articles++;
          const r = await downloadArticle(art, { zip: false });
          ok += r.ok;
          failed += r.failed;
          albumLogLine(`✓ ${title}：${r.ok} 张成功${r.failed ? "，" + r.failed + " 张失败" : ""}`);
        } catch (e) {
          failed++;
          albumLogLine(`✗ ${title}：${e.message}`, true);
        }
        await new Promise((r) => setTimeout(r, 400));
      }
      albumStatus.textContent = `完成：${articles}/${sel.length} 篇，${ok} 张成功${failed ? "，" + failed + " 张失败" : ""}`;
    } finally {
      busy = false;
      albumBatch.disabled = false;
      albumToggle.disabled = false;
      albumSyncCount();
    }
  }

  function mountAlbumMode() {
    const host = el("div");
    host.id = "wxmp-album-host";
    host.style.cssText = "position:fixed;z-index:2147483000;right:0;bottom:0;width:0;height:0;";
    document.documentElement.appendChild(host);
    const sh = host.attachShadow({ mode: "open" });
    const style = el("style");
    style.textContent = `
      :host { all: initial; }
      .album-fab { position: fixed; right: 22px; bottom: 22px; border:0; cursor:pointer; padding:11px 16px;
        border-radius:999px; background:linear-gradient(145deg,#12cc6b,#05a04c); color:#fff;
        font:600 13px/1 "PingFang SC","Microsoft YaHei",sans-serif; box-shadow:0 12px 30px -10px rgba(5,160,76,.7); }
      .bar.open { display:block; }
      .bar { display:none; position: fixed; right: 22px; bottom: 84px; width: 316px; background:#fff; border:1px solid #e2ebe6;
        border-radius:14px; box-shadow:0 18px 44px -18px rgba(10,45,30,.45); padding:12px 14px;
        font:13px/1.5 "PingFang SC","Microsoft YaHei",sans-serif; color:#16302b; }
      .hd { display:flex; align-items:center; gap:8px; font-weight:700; font-size:13px; }
      .hd .dot { width:8px; height:8px; border-radius:50%; background:#07c160; }
      .hd .n { margin-left:auto; color:#059a4c; font-weight:600; font-size:12px; }
      button { border:0; border-radius:9px; cursor:pointer; font:600 12.5px/1 inherit; font-family:inherit; }
      .toggle { width:100%; margin-top:10px; padding:9px 12px; background:#f2f9f5; color:#059a4c; border:1px solid #d9ece2; }
      .batch { width:100%; margin-top:8px; padding:10px 12px; background:linear-gradient(145deg,#12cc6b,#05a04c); color:#fff; display:none; }
      .batch:disabled { opacity:.5; cursor:not-allowed; }
      .status { margin-top:8px; font-size:11.5px; color:#7d918c; min-height:17px; }
      .log { margin-top:6px; max-height:96px; overflow:auto; background:#10201b; color:#cfe9d9; border-radius:8px;
        padding:6px 8px; font:10.5px/1.6 Consolas,monospace; white-space:pre-wrap; word-break:break-all; display:none; }
      .foot { margin-top:7px; font-size:10.5px; color:#9aada7; }
      .prog { height:6px; border-radius:999px; background:#edf4ef; overflow:hidden; margin-top:8px; }
      .prog i { display:block; height:100%; width:0; background:linear-gradient(90deg,#23d478,#06a851); transition:width .3s; }
      .dirrow { display:flex; align-items:center; gap:8px; margin-top:9px; }
      .dirrow button { padding:8px 10px; background:#f2f9f5; color:#059a4c; border:1px solid #d9ece2; white-space:nowrap; }
      .dirrow span { font-size:10.5px; color:#7d918c; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
      .sm { display:block; margin-top:8px; font-size:10.5px; color:#7d918c; }
      input { width:100%; box-sizing:border-box; margin-top:3px; padding:7px 8px; border:1px solid #d9e7df; border-radius:8px;
        font:12px/1.3 Consolas,monospace; color:#16302b; background:#fbfefc; }
    `;
    sh.appendChild(style);
    const fab = el("button", "album-fab", "合集批量下载");
    fab.type = "button";
    fab.addEventListener("click", () => albumBar.classList.toggle("open"));
    sh.appendChild(fab);
    albumBar = el("div", "bar");
    albumBar.innerHTML = `
      <div class="hd"><span class="dot"></span>合集批量下载<span class="n">已选 <b>0</b> 篇</span></div>
      <div class="dirrow"><button data-album-dir type="button">选择保存位置</button><span data-dirtext>默认：浏览器下载目录</span></div>
      <label class="sm">重命名规则（/ 可建子文件夹）</label>
      <input data-album-tpl type="text" spellcheck="false" placeholder="<title>-<post_id>-<order>.<ext>">
      <button class="toggle" type="button">进入多选模式</button>
      <button class="batch" type="button" disabled>批量下载所选文章</button>
      <div class="prog"><i></i></div>
      <div class="status">默认关闭；进入后勾选文章再批量下载。</div>
      <div class="log"></div>
      <div class="foot">只读取公开文章；图片直接保存到浏览器下载目录/已选文件夹。</div>`;
    sh.appendChild(albumBar);
    const albumDirBtn = albumBar.querySelector("[data-album-dir]");
    const albumDirText = albumBar.querySelector("[data-dirtext]");
    const albumTpl = albumBar.querySelector("[data-album-tpl]");
    albumTpl.value = cfg.template;
    albumTpl.addEventListener("input", () => { cfg.template = albumTpl.value; saveCfg(cfg); });
    albumDirBtn.addEventListener("click", chooseDir);
    albumToggle = albumBar.querySelector(".toggle");
    albumBatch = albumBar.querySelector(".batch");
    albumStatus = albumBar.querySelector(".status");
    albumLog = albumBar.querySelector(".log");
    albumCount = albumBar.querySelector(".n b");
    bar = albumBar.querySelector(".prog i");
    statusText = albumStatus;
    logBox = albumLog;
    albumToggle.addEventListener("click", () => {
      setMultiMode(!multiMode);
      albumStatus.textContent = multiMode ? "多选模式已开启：点击文章卡片勾选。" : "多选模式已关闭。";
    });
    albumBatch.addEventListener("click", albumBatchDownload);
    decorateAlbumItems();
    const obs = new MutationObserver(() => decorateAlbumItems());
    obs.observe(document.body, { childList: true, subtree: true });
  }

  if (isAlbumPage()) {
    mountAlbumMode();
  } else if (document.querySelector("#js_content")) {
    mount();
  }
})();
