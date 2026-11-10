package com.wxmp.downloader.core;

import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.HashMap;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** 命名模板渲染：支持中文别名，"/" 创建子文件夹。 */
public final class NameTemplate {
    public static final String DEFAULT = "<title>-<post_id>-<order>.<ext>";
    public static final String DEFAULT_COLLECTION = "<collection_title>/<collection_order>-<title>/<post_id>-<order>.<ext>";
    private static final Pattern VAR = Pattern.compile("<([^<>]+)>");
    private static final Map<String, String> ALIAS = new HashMap<>();

    static {
        ALIAS.put("文章标题", "<title>");
        ALIAS.put("文章id", "<post_id>");
        ALIAS.put("文章ID", "<post_id>");
        ALIAS.put("图片序号", "<order>");
        ALIAS.put("扩展名", "<ext>");
        ALIAS.put("公众号", "<account>");
        ALIAS.put("作者", "<author>");
        ALIAS.put("日期", "<date>");
        ALIAS.put("年", "<year>");
        ALIAS.put("月", "<month>");
        ALIAS.put("日", "<day>");
        ALIAS.put("类型", "<kind>");
        ALIAS.put("宽度", "<width>");
        ALIAS.put("高度", "<height>");
        ALIAS.put("合集标题", "<collection_title>");
        ALIAS.put("合集ID", "<collection_id>");
        ALIAS.put("合集id", "<collection_id>");
        ALIAS.put("合集内文章序号", "<collection_order>");
        ALIAS.put("合集序号", "<collection_order>");
    }

    private NameTemplate() {}

    public static String canonical(String token) {
        String raw = token.trim();
        if (raw.startsWith("<") && raw.endsWith(">")) raw = raw.substring(1, raw.length() - 1);
        String t = "<" + raw + ">";
        String alias = ALIAS.get(raw);
        if (alias != null) return alias;
        switch (t) {
            case "<title>": case "<post_id>": case "<order>": case "<order2>": case "<ext>":
            case "<account>": case "<author>": case "<date>": case "<year>": case "<month>":
            case "<day>": case "<kind>": case "<width>": case "<height>": case "<biz>":
            case "<sn>": case "<mid>": case "<idx>": case "<file_id>":
            case "<collection_title>": case "<collection_id>": case "<collection_order>":
                return t;
            default:
                return "";
        }
    }

    public static void validate(String tpl) throws IllegalArgumentException {
        String s = tpl == null ? "" : tpl.trim();
        if (s.isEmpty()) throw new IllegalArgumentException("命名规则不能为空");
        Matcher m = VAR.matcher(s);
        boolean found = false;
        while (m.find()) {
            found = true;
            if (canonical(m.group(1)).isEmpty()) {
                throw new IllegalArgumentException("未知变量 <" + m.group(1) + ">");
            }
        }
        if (!found) throw new IllegalArgumentException("命名规则里至少需要一个变量");
    }

    public static String build(Media m, String tpl) {
        String template = tpl == null || tpl.trim().isEmpty() ? DEFAULT : tpl.trim();
        String[] raw = template.split("[/\\\\]+");
        StringBuilder rel = new StringBuilder();
        String last = "";
        for (String seg : raw) {
            if (seg.isEmpty()) continue;
            String rendered = render(seg, m);
            rendered = rendered.replaceAll("[\\\\/:*?\"<>|\\u0000-\\u001f]", "_").trim();
            if (rendered.isEmpty()) continue;
            if (rel.length() > 0) rel.append('/');
            rel.append(rendered);
            last = rendered;
        }
        if (rel.length() == 0) rel.append(sanitize(m.title.isEmpty() ? "未命名" : m.title));
        if (!hasExt(template)) {
            rel.append('.').append(WxParser.normalizeExt(m.ext));
        }
        return rel.toString();
    }

    private static boolean hasExt(String template) {
        Matcher m = VAR.matcher(template);
        while (m.find()) {
            if ("<ext>".equals(canonical(m.group(1)))) return true;
        }
        return false;
    }

    private static String render(String seg, Media m) {
        Matcher matcher = VAR.matcher(seg);
        StringBuffer sb = new StringBuffer();
        while (matcher.find()) {
            String c = canonical(matcher.group(1));
            matcher.appendReplacement(sb, Matcher.quoteReplacement(c.isEmpty() ? "" : value(c, m)));
        }
        matcher.appendTail(sb);
        return sb.toString();
    }

    private static String value(String token, Media m) {
        switch (token) {
            case "<title>": return m.title;
            case "<post_id>": return m.postId;
            case "<order>": return String.valueOf(m.seq);
            case "<order2>": return String.format(Locale.ROOT, "%02d", m.seq);
            case "<ext>": return WxParser.normalizeExt(m.ext);
            case "<account>": return m.account;
            case "<author>": return m.author;
            case "<date>": return date(m.publishTime, "yyyy-MM-dd");
            case "<year>": return date(m.publishTime, "yyyy");
            case "<month>": return date(m.publishTime, "MM");
            case "<day>": return date(m.publishTime, "dd");
            case "<kind>": return m.kind;
            case "<width>": return String.valueOf(m.width);
            case "<height>": return String.valueOf(m.height);
            case "<biz>": return m.biz;
            case "<sn>": return m.sn;
            case "<mid>": return m.mid;
            case "<idx>": return m.idx;
            case "<file_id>": return m.fileId;
            case "<collection_title>": return m.collectionTitle;
            case "<collection_id>": return m.collectionId;
            case "<collection_order>": return m.collectionOrder > 0 ? String.valueOf(m.collectionOrder) : "";
            default: return "";
        }
    }

    private static String date(long ts, String pattern) {
        Date d = ts > 0 ? new Date(ts * 1000L) : new Date();
        return new SimpleDateFormat(pattern, Locale.CHINA).format(d);
    }

    public static String sanitize(String seg) {
        String s = seg == null ? "" : seg;
        s = s.replaceAll("[\\\\/:*?\"<>|\\u0000-\\u001f]", "_").replaceAll("\\s+", " ").trim();
        while (s.endsWith(".") || s.endsWith(" ")) s = s.substring(0, s.length() - 1);
        if (s.isEmpty() || s.equals(".") || s.equals("..")) s = "_";
        if (s.matches("(?i)con|prn|aux|nul|com[1-9]|lpt[1-9]")) s = "_" + s;
        return s.length() > 100 ? s.substring(0, 100) : s;
    }
}
