package com.wxmp.downloader.core;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.zip.GZIPInputStream;

/** 极简 HTTP 客户端：只用 HttpURLConnection，无第三方依赖。 */
public final class Http {
    public static final String UA =
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
                    + "(KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36";
    public static final String REFERER = "https://mp.weixin.qq.com/";
    private static final int MAX_BYTES = 20 * 1024 * 1024;

    private Http() {}

    public static byte[] get(String rawUrl) throws IOException {
        IOException last = null;
        for (int attempt = 1; attempt <= 3; attempt++) {
            try {
                return getOnce(rawUrl);
            } catch (IOException e) {
                last = e;
                try {
                    Thread.sleep(400L * attempt);
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                    throw new IOException("已取消");
                }
            }
        }
        throw last == null ? new IOException("下载失败") : last;
    }

    private static byte[] getOnce(String rawUrl) throws IOException {
        HttpURLConnection conn = null;
        try {
            URL url = new URL(rawUrl);
            conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(20000);
            conn.setReadTimeout(60000);
            conn.setInstanceFollowRedirects(true);
            conn.setRequestProperty("User-Agent", UA);
            conn.setRequestProperty("Referer", REFERER);
            conn.setRequestProperty("Accept", "image/avif,image/webp,image/png,image/*,*/*;q=0.8");
            conn.setRequestProperty("Accept-Language", "zh-CN,zh;q=0.9");
            conn.setRequestProperty("Accept-Encoding", "gzip");
            int code = conn.getResponseCode();
            if (code < 200 || code >= 300) {
                throw new IOException("HTTP " + code);
            }
            InputStream in = conn.getInputStream();
            String enc = conn.getContentEncoding();
            if (enc != null && enc.toLowerCase().contains("gzip")) {
                in = new GZIPInputStream(in);
            }
            try {
                ByteArrayOutputStream out = new ByteArrayOutputStream(64 * 1024);
                byte[] buf = new byte[16 * 1024];
                int n;
                int total = 0;
                while ((n = in.read(buf)) > 0) {
                    total += n;
                    if (total > MAX_BYTES) {
                        throw new IOException("内容超过 20MB");
                    }
                    out.write(buf, 0, n);
                }
                return out.toByteArray();
            } finally {
                try { in.close(); } catch (IOException ignored) {}
            }
        } finally {
            if (conn != null) conn.disconnect();
        }
    }

    public static String text(byte[] data) {
        String s = new String(data, java.nio.charset.StandardCharsets.UTF_8);
        if (s.startsWith("\uFEFF")) s = s.substring(1);
        return s;
    }
}
