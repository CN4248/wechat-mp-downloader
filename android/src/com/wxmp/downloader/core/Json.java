package com.wxmp.downloader.core;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** 极简 JSON 解析器：只服务合集分页，避免引入第三方依赖。 */
public final class Json {
    private Json() {}

    public static Object parse(String s) {
        Parser p = new Parser(s == null ? "" : s);
        Object v = p.value();
        return v;
    }

    @SuppressWarnings("unchecked")
    public static Map<String, Object> object(Object o) {
        return o instanceof Map ? (Map<String, Object>) o : new LinkedHashMap<String, Object>();
    }

    @SuppressWarnings("unchecked")
    public static List<Object> array(Object o) {
        return o instanceof List ? (List<Object>) o : new ArrayList<Object>();
    }

    public static String str(Object o) {
        return o == null ? "" : String.valueOf(o);
    }

    private static final class Parser {
        private final String s;
        private int i;

        Parser(String s) { this.s = s; }

        Object value() {
            skip();
            if (i >= s.length()) return "";
            char c = s.charAt(i);
            if (c == '{') return obj();
            if (c == '[') return arr();
            if (c == '"') return string();
            if (s.startsWith("true", i)) { i += 4; return "true"; }
            if (s.startsWith("false", i)) { i += 5; return "false"; }
            if (s.startsWith("null", i)) { i += 4; return ""; }
            return number();
        }

        Map<String, Object> obj() {
            Map<String, Object> m = new LinkedHashMap<>();
            i++;
            while (i < s.length()) {
                skip();
                if (i < s.length() && s.charAt(i) == '}') { i++; break; }
                String k = string();
                skip();
                if (i < s.length() && s.charAt(i) == ':') i++;
                m.put(k, value());
                skip();
                if (i < s.length() && s.charAt(i) == ',') i++;
            }
            return m;
        }

        List<Object> arr() {
            List<Object> l = new ArrayList<>();
            i++;
            while (i < s.length()) {
                skip();
                if (i < s.length() && s.charAt(i) == ']') { i++; break; }
                l.add(value());
                skip();
                if (i < s.length() && s.charAt(i) == ',') i++;
            }
            return l;
        }

        String string() {
            skip();
            if (i >= s.length() || s.charAt(i) != '"') return "";
            i++;
            StringBuilder b = new StringBuilder();
            while (i < s.length()) {
                char c = s.charAt(i++);
                if (c == '"') break;
                if (c == '\\' && i < s.length()) {
                    char e = s.charAt(i++);
                    switch (e) {
                        case 'n': b.append('\n'); break;
                        case 't': b.append('\t'); break;
                        case 'r': b.append('\r'); break;
                        case 'b': b.append('\b'); break;
                        case 'f': b.append('\f'); break;
                        case 'u':
                            if (i + 4 <= s.length()) {
                                b.append((char) Integer.parseInt(s.substring(i, i + 4), 16));
                                i += 4;
                            }
                            break;
                        default: b.append(e);
                    }
                } else {
                    b.append(c);
                }
            }
            return b.toString();
        }

        String number() {
            int start = i;
            while (i < s.length() && ",}] \n\r\t".indexOf(s.charAt(i)) < 0) i++;
            return s.substring(start, i);
        }

        void skip() {
            while (i < s.length() && Character.isWhitespace(s.charAt(i))) i++;
        }
    }
}
