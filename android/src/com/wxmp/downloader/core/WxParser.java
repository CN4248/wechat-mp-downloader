package com.wxmp.downloader.core;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** 解析公众号文章 HTML，提取原图 / 动图地址与文章元数据。 */
public final class WxParser {
    private static final Pattern IMG = Pattern.compile("<img\\b[^>]*>", Pattern.CASE_INSENSITIVE);
    private static final Pattern ATTR = Pattern.compile(
            "([\\w:-]+)\\s*=\\s*(\"([^\"]*)\"|'([^']*)'|([^\\s>]+))");

    private WxParser() {}

    public static Article parse(String html, String pageUrl) {
        Article a = new Article();
        a.url = pageUrl == null ? "" : pageUrl;
        a.title = clean(first(grabVar(html, "msg_title"), meta(html, "og:title"), title(html)));
        if (a.title.isEmpty()) a.title = "未命名文章";
        a.account = clean(grabVar(html, "nickname"));
        a.author = clean(grabVar(html, "author"));
        a.postId = first(pathId(pageUrl), grabVar(html, "appmsgid"), grabVar(html, "mid"));
        a.biz = grabVar(html, "biz");
        a.sn = grabVar(html, "sn");
        a.mid = first(grabVar(html, "mid"), grabVar(html, "appmsgid"));
        a.idx = grabVar(html, "idx");
        a.publishTime = parseLong(grabVar(html, "ct"));
        a.coverUrl = decode(grabVar(html, "msg_cdn_url"));

        String content = contentSlice(html);
        if (content.isEmpty()) content = html;
        Set<String> seen = new LinkedHashSet<>();
        List<Media> list = new ArrayList<>();
        List<int[]> contentDims = new ArrayList<>();

        Matcher m = IMG.matcher(content);
        while (m.find()) {
            String tag = m.group();
            String raw = attr(tag, "data-src");
            if (raw.isEmpty()) raw = attr(tag, "src");
            if (raw.isEmpty() || raw.startsWith("data:")) continue;
            String lower = raw.toLowerCase(Locale.ROOT);
            if (!lower.contains("qpic.cn") && !lower.contains("qlogo.cn")
                    && !lower.contains("mmbiz") && !lower.contains("wx_fmt=")) continue;
            String type = attr(tag, "data-type").toLowerCase(Locale.ROOT);
            String ext = extFromUrl(raw, type);
            boolean gif = type.equals("gif") || ext.equals("gif") || lower.contains("mmbiz_gif");
            if (gif) ext = "gif";
            int w = parseInt(attr(tag, "data-w"));
            if (w == 0) w = parseInt(attr(tag, "width"));
            int h = parseInt(attr(tag, "height"));
            if (h == 0) {
                double ratio = parseDouble(attr(tag, "data-ratio"));
                h = (w > 0 && ratio > 0) ? (int) Math.round(w * ratio) : 0;
            }
            String orig = originalUrl(raw, ext);
            if (orig.isEmpty()) continue;
            String key = cdnKey(orig);
            if (!seen.add(key)) continue;
            Media md = new Media();
            md.kind = gif ? "gif" : "image";
            md.url = orig;
            md.previewUrl = previewUrl(raw, 300);
            md.ext = ext;
            md.width = w;
            md.height = h;
            md.gif = gif;
            md.fileId = attr(tag, "data-imgfileid");
            md.source = "content";
            list.add(md);
            if (w > 0 && h > 0) contentDims.add(new int[]{w, h});
        }

        List<Media> pics = pictureList(html);
        boolean doubleMirror = !contentDims.isEmpty() && pics.size() == 2 * contentDims.size();
        boolean mirror = !pics.isEmpty() && (pics.size() == contentDims.size() || doubleMirror);
        int mirrorSkipped = 0;
        for (int pi = 0; pi < pics.size(); pi++) {
            Media p = pics.get(pi);
            int mi = doubleMirror ? pi % contentDims.size() : pi;
            String ext = extFromUrl(p.url, "");
            String orig = originalUrl(p.url, ext);
            if (orig.isEmpty()) continue;
            if (mirror && mi < contentDims.size()
                    && p.width == contentDims.get(mi)[0] && p.height == contentDims.get(mi)[1]) {
                if (!seen.contains(cdnKey(orig))) mirrorSkipped++;
                continue;
            }
            if (!seen.add(cdnKey(orig))) continue;
            p.kind = ext.equals("gif") ? "gif" : "image";
            p.url = orig;
            p.ext = ext;
            p.gif = ext.equals("gif");
            p.source = "picture";
            list.add(p);
        }
        if (mirrorSkipped > 0) a.warnings.add("图集与正文重复，已忽略 " + mirrorSkipped + " 张");

        int seq = 1;
        for (Media md : list) {
            md.seq = seq++;
            md.title = a.title;
            md.postId = a.postId;
            md.account = a.account;
            md.author = a.author;
            md.biz = a.biz;
            md.sn = a.sn;
            md.mid = a.mid;
            md.idx = a.idx;
            md.publishTime = a.publishTime;
            md.articleUrl = a.url;
            a.medias.add(md);
        }
        if (list.isEmpty()) a.warnings.add("没有在页面中找到图片");
        return a;
    }

    /* ---------- 页面切块与变量 ---------- */

    public static String contentSlice(String html) {
        int i = html.indexOf("id=\"js_content\"");
        if (i < 0) return "";
        int start = html.lastIndexOf("<div", i);
        if (start < 0) return "";
        int depth = 1;
        int p = start + 4;
        while (p < html.length()) {
            if (region(html, p, "<script")) {
                int e = indexOfIgnoreCase(html, "</script", p);
                if (e < 0) break;
                p = e + 8;
            } else if (region(html, p, "<div")) {
                depth++;
                p += 4;
            } else if (region(html, p, "</div")) {
                depth--;
                int end = html.indexOf('>', p);
                if (end < 0) end = html.length() - 1;
                if (depth == 0) return html.substring(start, end + 1);
                p = end + 1;
            } else {
                p++;
            }
        }
        return html.substring(start);
    }

    static String grabVar(String html, String name) {
        Pattern re = Pattern.compile("(?:var\\s+|window\\.)" + Pattern.quote(name) + "\\s*=\\s*");
        Matcher m = re.matcher(html);
        while (m.find()) {
            String v = readValue(html.substring(m.end()));
            if (v != null && !v.isEmpty()) return v;
        }
        return "";
    }

    private static String readValue(String s) {
        int i = 0;
        while (i < s.length() && Character.isWhitespace(s.charAt(i))) i++;
        if (i >= s.length()) return "";
        char c = s.charAt(i);
        if (c == '\'' || c == '"') return readString(s.substring(i));
        if (c == '-' || Character.isDigit(c)) {
            Matcher m = Pattern.compile("-?\\d+").matcher(s.substring(i));
            return m.find() ? m.group() : "";
        }
        if (s.startsWith("htmlDecode(", i)) {
            int j = i + "htmlDecode(".length();
            while (j < s.length() && Character.isWhitespace(s.charAt(j))) j++;
            if (j < s.length() && (s.charAt(j) == '\'' || s.charAt(j) == '"')) {
                return decode(readString(s.substring(j)));
            }
        }
        return "";
    }

    private static String readString(String s) {
        if (s.isEmpty()) return "";
        char q = s.charAt(0);
        StringBuilder out = new StringBuilder();
        for (int i = 1; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == '\\') {
                if (i + 1 >= s.length()) break;
                char n = s.charAt(++i);
                if (n == 'n') out.append('\n');
                else if (n == 't') out.append('\t');
                else if (n == 'r') out.append('\r');
                else if (n == 'u' && i + 4 < s.length()) {
                    out.append((char) Integer.parseInt(s.substring(i + 1, i + 5), 16));
                    i += 4;
                } else if (n == 'x' && i + 2 < s.length()) {
                    out.append((char) Integer.parseInt(s.substring(i + 1, i + 3), 16));
                    i += 2;
                } else out.append(n);
            } else if (c == q) {
                break;
            } else {
                out.append(c);
            }
        }
        return out.toString();
    }

    private static List<Media> pictureList(String html) {
        List<Media> out = new ArrayList<>();
        int i = html.indexOf("picture_page_info_list");
        if (i < 0) return out;
        int b = html.indexOf('[', i);
        if (b < 0) return out;
        int depth = 0;
        int end = -1;
        for (int p = b; p < html.length(); p++) {
            char c = html.charAt(p);
            if (c == '[') depth++;
            else if (c == ']') {
                depth--;
                if (depth == 0) { end = p; break; }
            }
        }
        if (end < 0) return out;
        String arr = html.substring(b + 1, end);
        int brace = 0;
        int objStart = -1;
        for (int p = 0; p < arr.length(); p++) {
            char c = arr.charAt(p);
            if (c == '{') {
                if (brace == 0) objStart = p;
                brace++;
            } else if (c == '}') {
                brace--;
                if (brace == 0 && objStart >= 0) {
                    String obj = arr.substring(objStart, p + 1);
                    Matcher u = Pattern.compile("cdn_url\\s*:\\s*(['\"])([^'\"]+)\\1").matcher(obj);
                    if (u.find()) {
                        Media md = new Media();
                        md.url = decode(u.group(2));
                        md.width = parseInt(field(obj, "width"));
                        md.height = parseInt(field(obj, "height"));
                        out.add(md);
                    }
                    objStart = -1;
                }
            }
        }
        return out;
    }

    private static String field(String obj, String key) {
        Matcher m = Pattern.compile(key + "\\s*:\\s*['\"]?(\\d+)").matcher(obj);
        return m.find() ? m.group(1) : "";
    }

    /* ---------- URL ---------- */

    public static String originalUrl(String raw, String ext) {
        String s = decode(raw).trim();
        if (s.startsWith("//")) s = "https:" + s;
        if (!s.startsWith("http://") && !s.startsWith("https://")) return "";
        int q = s.indexOf('?');
        String base = q >= 0 ? s.substring(0, q) : s;
        String query = q >= 0 ? s.substring(q + 1) : "";
        int slash = base.lastIndexOf('/');
        if (slash > 0) {
            String last = base.substring(slash + 1);
            if (last.matches("\\d{2,4}")) base = base.substring(0, slash) + "/0";
        }
        StringBuilder qs = new StringBuilder();
        String wx = normalizeExt(ext).equals("jpg") ? "jpeg" : normalizeExt(ext);
        boolean hasFmt = false;
        for (String pair : query.split("&")) {
            if (pair.isEmpty()) continue;
            int eq = pair.indexOf('=');
            String k = eq >= 0 ? pair.substring(0, eq) : pair;
            if (k.equals("tp")) continue;
            if (k.equals("wx_fmt")) {
                if (!wx.isEmpty() && !wx.equals("bin")) {
                    qs.append("wx_fmt=").append(wx).append('&');
                    hasFmt = true;
                }
                continue;
            }
            qs.append(pair).append('&');
        }
        if (!hasFmt && !wx.isEmpty() && !wx.equals("bin")) qs.append("wx_fmt=").append(wx);
        String out = qs.toString();
        if (out.endsWith("&")) out = out.substring(0, out.length() - 1);
        return out.isEmpty() ? base : base + "?" + out;
    }

    static String previewUrl(String raw, int size) {
        String s = decode(raw).trim();
        if (s.startsWith("//")) s = "https:" + s;
        int q = s.indexOf('?');
        String base = q >= 0 ? s.substring(0, q) : s;
        String query = q >= 0 ? s.substring(q + 1) : "";
        int slash = base.lastIndexOf('/');
        if (slash > 0 && base.substring(slash + 1).matches("\\d{2,4}")) {
            base = base.substring(0, slash) + "/" + size;
        }
        return query.isEmpty() ? base : base + "?" + query;
    }

    public static String cdnKey(String raw) {
        try {
            java.net.URL u = new java.net.URL(decode(raw));
            String path = u.getPath();
            path = path.replaceAll("^/+|/+$", "");
            String[] segs = path.split("/");
            int n = segs.length;
            if (n > 0 && segs[n - 1].matches("\\d{1,4}")) n--;
            // 用完整文件名去重：不同图片可能共享前 33 个字符，截前缀会误合并；
            // 同一张图的正文/图集变体只是目录不同，文件名完全一致。
            if (n > 0 && segs[n - 1].length() >= 24) {
                return "h:" + segs[n - 1].toLowerCase(Locale.ROOT);
            }
            StringBuilder sb = new StringBuilder();
            for (int i = 0; i < n; i++) sb.append(i == 0 ? "" : "/").append(segs[i]);
            return (u.getHost() == null ? "" : u.getHost().toLowerCase(Locale.ROOT)) + "/" + sb;
        } catch (Exception e) {
            return raw;
        }
    }

    public static String extFromUrl(String raw, String dataType) {
        String low = raw == null ? "" : raw.toLowerCase(Locale.ROOT);
        Matcher m = Pattern.compile("[?&]wx_fmt=([a-z0-9]+)").matcher(low);
        if (m.find()) {
            String v = m.group(1);
            if (v.equals("jpeg") || v.equals("jpg")) return "jpg";
            if (v.equals("png") || v.equals("gif") || v.equals("webp") || v.equals("bmp")) return v;
        }
        String t = dataType == null ? "" : dataType.toLowerCase(Locale.ROOT);
        if (t.equals("jpeg") || t.equals("jpg")) return "jpg";
        if (t.equals("png") || t.equals("gif") || t.equals("webp") || t.equals("bmp")) return t;
        String[] kws = {"mmbiz_gif", "mmbiz_png", "mmbiz_jpg", "mmbiz_webp"};
        for (String kw : kws) {
            if (low.contains(kw)) return kw.substring(6);
        }
        Matcher f = Pattern.compile("\\.(png|jpe?g|gif|webp|bmp)(?:[?#]|$)").matcher(low);
        if (f.find()) return normalizeExt(f.group(1));
        return "jpg";
    }

    public static String normalizeExt(String ext) {
        String e = ext == null ? "" : ext.toLowerCase(Locale.ROOT).trim();
        if (e.startsWith(".")) e = e.substring(1);
        if (e.equals("jpeg")) e = "jpg";
        if (e.equals("svg+xml")) e = "svg";
        return e.isEmpty() ? "jpg" : e;
    }

    /* ---------- 杂项 ---------- */

    private static String attr(String tag, String name) {
        Matcher m = ATTR.matcher(tag);
        while (m.find()) {
            if (m.group(1).equalsIgnoreCase(name)) {
                String v = m.group(3) != null ? m.group(3) : (m.group(4) != null ? m.group(4) : m.group(5));
                return decode(v == null ? "" : v);
            }
        }
        return "";
    }

    public static String decode(String s) {
        if (s == null) return "";
        String out = s.replace("\\x26", "&").replace("\\/", "/");
        out = out.replace("&amp;", "&").replace("&lt;", "<").replace("&gt;", ">")
                .replace("&quot;", "\"").replace("&#39;", "'").replace("&apos;", "'");
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < out.length(); i++) {
            char c = out.charAt(i);
            if (c == '&' && i + 2 < out.length() && out.charAt(i + 1) == '#') {
                int semi = out.indexOf(';', i);
                if (semi > i && semi - i <= 9) {
                    String num = out.substring(i + 2, semi);
                    try {
                        int cp = num.startsWith("x") || num.startsWith("X")
                                ? Integer.parseInt(num.substring(1), 16)
                                : Integer.parseInt(num);
                        sb.appendCodePoint(cp);
                        i = semi;
                        continue;
                    } catch (Exception ignored) {}
                }
            }
            sb.append(c);
        }
        return sb.toString();
    }

    private static String clean(String s) {
        return decode(s == null ? "" : s.replaceAll("<[^>]*>", "")).trim();
    }

    private static String meta(String html, String prop) {
        Matcher m = Pattern.compile("<meta[^>]+(?:property|name)=[\"']" + Pattern.quote(prop)
                + "[\"'][^>]*>", Pattern.CASE_INSENSITIVE).matcher(html);
        if (!m.find()) return "";
        return attr(m.group(), "content");
    }

    private static String title(String html) {
        Matcher m = Pattern.compile("<title[^>]*>([\\s\\S]*?)</title>", Pattern.CASE_INSENSITIVE).matcher(html);
        return m.find() ? clean(m.group(1)) : "";
    }

    private static String pathId(String url) {
        if (url == null) return "";
        Matcher m = Pattern.compile("/s/([A-Za-z0-9_-]{6,})").matcher(url);
        return m.find() ? m.group(1) : "";
    }

    private static boolean region(String s, int at, String what) {
        return s.regionMatches(true, at, what, 0, what.length());
    }

    private static int indexOfIgnoreCase(String s, String what, int from) {
        for (int i = from; i <= s.length() - what.length(); i++) {
            if (s.regionMatches(true, i, what, 0, what.length())) return i;
        }
        return -1;
    }

    private static String first(String... vals) {
        for (String v : vals) if (v != null && !v.trim().isEmpty()) return v.trim();
        return "";
    }

    static int parseInt(String s) {
        try { return Integer.parseInt(s.trim()); } catch (Exception e) { return 0; }
    }

    private static double parseDouble(String s) {
        try { return Double.parseDouble(s.trim()); } catch (Exception e) { return 0; }
    }

    private static long parseLong(String s) {
        try { return Long.parseLong(s.trim()); } catch (Exception e) { return 0L; }
    }
}
