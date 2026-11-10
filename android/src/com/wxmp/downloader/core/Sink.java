package com.wxmp.downloader.core;

import java.io.IOException;
import java.io.OutputStream;

/** 下载落盘抽象：普通目录或 SAF 目录树。 */
public interface Sink {
    boolean exists(String relPath) throws IOException;

    OutputStream open(String relPath) throws IOException;

    String describe();

    static String mime(String relPath) {
        String lower = relPath.toLowerCase();
        if (lower.endsWith(".png")) return "image/png";
        if (lower.endsWith(".gif")) return "image/gif";
        if (lower.endsWith(".webp")) return "image/webp";
        if (lower.endsWith(".bmp")) return "image/bmp";
        return "image/jpeg";
    }
}
