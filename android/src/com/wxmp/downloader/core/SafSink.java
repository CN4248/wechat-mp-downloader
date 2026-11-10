package com.wxmp.downloader.core;

import android.content.ContentResolver;
import android.content.Context;
import android.net.Uri;

import java.io.IOException;
import java.io.OutputStream;

/** SAF 目录树写入，使用自带的轻量 DocFile 封装，避免引入 androidx 依赖。 */
public class SafSink implements Sink {
    private final ContentResolver resolver;
    private final DocFile root;

    public SafSink(Context ctx, Uri treeUri) {
        this.resolver = ctx.getContentResolver();
        this.root = DocFile.fromTreeUri(ctx, treeUri);
    }

    @Override
    public boolean exists(String rel) throws IOException {
        String[] parts = split(rel);
        DocFile dir = walk(parts, false);
        if (dir == null) return false;
        DocFile f = dir.findFile(parts[parts.length - 1]);
        return f != null && f.isFile() && f.length() > 0;
    }

    @Override
    public OutputStream open(String rel) throws IOException {
        String[] parts = split(rel);
        DocFile dir = walk(parts, true);
        if (dir == null) throw new IOException("无法创建目录");
        String name = parts[parts.length - 1];
        DocFile f = dir.findFile(name);
        if (f != null && f.isFile()) f.delete();
        f = dir.createFile(Sink.mime(name), name);
        if (f == null) throw new IOException("无法创建文件: " + name);
        OutputStream out = resolver.openOutputStream(f.uri());
        if (out == null) throw new IOException("无法写入: " + name);
        return out;
    }

    private String[] split(String rel) {
        return rel.split("/");
    }

    private DocFile walk(String[] parts, boolean create) throws IOException {
        DocFile dir = root;
        for (int i = 0; i < parts.length - 1; i++) {
            String seg = NameTemplate.sanitize(parts[i]);
            DocFile next = dir.findFile(seg);
            if (next == null) {
                if (!create) return null;
                next = dir.createDirectory(seg);
            }
            if (next == null || !next.isDirectory()) return null;
            dir = next;
        }
        return dir;
    }

    @Override
    public String describe() {
        return root == null ? "SAF" : root.uri().toString();
    }
}
