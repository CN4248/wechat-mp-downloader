package com.wxmp.downloader.core;

import java.util.List;
import java.util.Map;

import java.net.URLEncoder;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** 解析公众号合集首页与分页 JSON。 */
public final class AlbumParser {
    private static final Pattern APPMSG_ALBUM = Pattern.compile("/mp/appmsgalbum");
    private static final Pattern ALBUM_ID = Pattern.compile("[?&]album_id=([0-9]+)");
    private static final Pattern BIZ = Pattern.compile("[?&]__biz=([^&]+)");
    private static final Pattern KEY = Pattern.compile("[\"']?([A-Za-z_][A-Za-z0-9_]*)[\"']?\\s*:");

    private AlbumParser() {}

    public static boolean isAlbum(String raw) {
        if (raw == null) return false;
        try {
            java.net.URI u = java.net.URI.create(raw.trim());
            String host = u.getHost() == null ? "" : u.getHost();
            return (host.endsWith("mp.weixin.qq.com") || host.endsWith("weixin.qq.com"))
                    && APPMSG_ALBUM.matcher(u.getPath() == null ? "" : u.getPath()).find()
                    && u.getQuery() != null && u.getQuery().contains("album_id=");
        } catch (Exception e) {
            return raw.contains("/mp/appmsgalbum") && raw.contains("album_id=");
        }
    }

    public static String albumId(String raw) {
        Matcher m = ALBUM_ID.matcher(raw == null ? "" : raw);
        return m.find() ? m.group(1) : "";
    }

    public static Album fetch(String raw) throws Exception {
        return fetch(raw, 300);
    }

    public static Album fetch(String raw, int max) throws Exception {
        String url = raw == null ? "" : raw.trim();
        byte[] data = Http.get(url);
        String html = Http.text(data);
        Album album = new Album();
        album.url = url;
        album.id = albumId(url);
        String cgi = objectAfter(html, "window.cgiData");
        album.title = clean(field(cgi, "title"));
        album.account = clean(field(cgi, "nick_name"));
        album.description = clean(field(cgi, "desc"));
        album.articleCount = WxParser.parseInt(field(cgi, "article_count"));
        String list = arrayAfter(cgi, "articleList");
        addObjects(album, list, max);
        if (album.title.isEmpty()) album.title = "公众号合集 " + album.id;

        String biz = query(url, "__biz");
        if (biz.isEmpty()) {
            album.warnings.add("链接里没有 __biz，无法分页，只解析到首页");
            return album;
        }
        while (album.articles.size() < max) {
            AlbumArticle last = album.articles.isEmpty() ? null : album.articles.get(album.articles.size() - 1);
            if (last == null) break;
            String api = "https://mp.weixin.qq.com/mp/appmsgalbum?action=getalbum&__biz="
                    + URLEncoder.encode(biz, "UTF-8")
                    + "&album_id=" + URLEncoder.encode(album.id, "UTF-8")
                    + "&count=20&begin_msgid=" + URLEncoder.encode(last.msgid, "UTF-8")
                    + "&begin_itemidx=" + URLEncoder.encode(last.itemidx, "UTF-8") + "&f=json";
            byte[] jb = Http.get(api);
            Map<String, Object> root = Json.object(Json.parse(Http.text(jb)));
            Object respObj = root.get("getalbum_resp");
            if (!(respObj instanceof Map)) {
                album.warnings.add("合集分页返回异常");
                break;
            }
            Map<String, Object> resp = Json.object(respObj);
            List<Object> arr = Json.array(resp.get("article_list"));
            int before = album.articles.size();
            for (int i = 0; i < arr.size() && album.articles.size() < max; i++) {
                Object o = arr.get(i);
                if (o instanceof Map) album.articles.add(fromJson(Json.object(o)));
            }
            String cont = Json.str(resp.get("continue_flag"));
            if (album.articles.size() == before || !"1".equals(cont)) break;
        }
        if (album.articleCount > 0 && album.articles.size() < album.articleCount) {
            album.warnings.add("合集共 " + album.articleCount + " 篇，本次解析到 " + album.articles.size() + " 篇");
        }
        return album;
    }

    private static AlbumArticle fromJson(Map<String, Object> o) {
        AlbumArticle a = new AlbumArticle();
        a.title = Json.str(o.get("title"));
        a.url = Json.str(o.get("url")).replaceFirst("^http://", "https://");
        a.msgid = Json.str(o.get("msgid"));
        a.itemidx = Json.str(o.get("itemidx"));
        a.order = WxParser.parseInt(Json.str(o.get("pos_num")));
        a.publishTime = parseLong(Json.str(o.get("create_time")));
        a.cover = Json.str(o.get("cover_img_1_1"));
        return a;
    }

    private static void addObjects(Album album, String list, int max) {
        int depth = 0, start = -1;
        boolean inStr = false, esc = false;
        for (int i = 0; i < list.length() && album.articles.size() < max; i++) {
            char c = list.charAt(i);
            if (inStr) {
                if (esc) esc = false;
                else if (c == '\\') esc = true;
                else if (c == '"') inStr = false;
                continue;
            }
            if (c == '"') { inStr = true; continue; }
            if (c == '{') {
                if (depth == 0) start = i;
                depth++;
            } else if (c == '}') {
                depth--;
                if (depth == 0 && start >= 0) {
                    String obj = list.substring(start, i + 1);
                    AlbumArticle a = new AlbumArticle();
                    a.title = clean(field(obj, "title"));
                    a.url = clean(field(obj, "url")).replaceFirst("^http://", "https://");
                    a.msgid = clean(field(obj, "msgid"));
                    a.itemidx = clean(field(obj, "itemidx"));
                    a.order = WxParser.parseInt(field(obj, "pos_num"));
                    a.publishTime = parseLong(field(obj, "create_time"));
                    a.cover = clean(field(obj, "cover_img_1_1"));
                    if (!a.url.isEmpty()) {
                        if (a.order <= 0 && album.articleCount > 0) {
                            a.order = album.articleCount - album.articles.size();
                        }
                        album.articles.add(a);
                    }
                }
            }
        }
    }

    static String query(String url, String key) {
        try {
            java.net.URI u = java.net.URI.create(url);
            String q = u.getRawQuery();
            if (q == null) return "";
            for (String part : q.split("&")) {
                int i = part.indexOf('=');
                if (i > 0 && part.substring(0, i).equals(key)) {
                    return java.net.URLDecoder.decode(part.substring(i + 1), "UTF-8");
                }
            }
        } catch (Exception ignored) {}
        return "";
    }

    static String objectAfter(String html, String marker) {
        int at = html.indexOf(marker);
        if (at < 0) return "";
        int start = html.indexOf('{', at);
        if (start < 0) return "";
        int depth = 0;
        boolean inStr = false, esc = false;
        for (int i = start; i < html.length(); i++) {
            char c = html.charAt(i);
            if (inStr) {
                if (esc) esc = false;
                else if (c == '\\') esc = true;
                else if (c == '"') inStr = false;
                continue;
            }
            if (c == '"') inStr = true;
            else if (c == '{') depth++;
            else if (c == '}') {
                depth--;
                if (depth == 0) return html.substring(start, i + 1);
            }
        }
        return "";
    }

    static String arrayAfter(String obj, String key) {
        Matcher m = Pattern.compile("[\"']?" + key + "[\"']?\\s*:\\s*\\[").matcher(obj);
        if (!m.find()) return "";
        int start = m.end() - 1;
        int depth = 0;
        boolean inStr = false, esc = false;
        for (int i = start; i < obj.length(); i++) {
            char c = obj.charAt(i);
            if (inStr) {
                if (esc) esc = false;
                else if (c == '\\') esc = true;
                else if (c == '"') inStr = false;
                continue;
            }
            if (c == '"') inStr = true;
            else if (c == '[') depth++;
            else if (c == ']') {
                depth--;
                if (depth == 0) return obj.substring(start, i + 1);
            }
        }
        return "";
    }

    static String field(String obj, String key) {
        if (obj == null || obj.isEmpty()) return "";
        Matcher m = KEY.matcher(obj);
        while (m.find()) {
            if (!m.group(1).equals(key)) continue;
            int i = m.end();
            while (i < obj.length() && Character.isWhitespace(obj.charAt(i))) i++;
            if (i >= obj.length()) return "";
            char c = obj.charAt(i);
            if (c == '"' || c == '\'') {
                StringBuilder b = new StringBuilder();
                boolean esc = false;
                for (int j = i + 1; j < obj.length(); j++) {
                    char x = obj.charAt(j);
                    if (esc) {
                        b.append(x == 'n' ? '\n' : x == 't' ? '\t' : x);
                        esc = false;
                    } else if (x == '\\') {
                        esc = true;
                    } else if (x == c) {
                        return b.toString();
                    } else {
                        b.append(x);
                    }
                }
                return b.toString();
            }
            int end = i;
            while (end < obj.length() && ",}] \n\r\t".indexOf(obj.charAt(end)) < 0) end++;
            return obj.substring(i, end);
        }
        return "";
    }

    static String clean(String s) { return WxParser.decode(s == null ? "" : s).trim(); }

    static long parseLong(String s) {
        try { return Long.parseLong(s.trim()); } catch (Exception e) { return 0; }
    }
}
