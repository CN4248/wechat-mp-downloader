package com.wxmp.downloader.core;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.OutputStream;

public class FileSink implements Sink {
    private final File root;

    public FileSink(File root) {
        this.root = root;
    }

    private File resolve(String rel) throws IOException {
        File f = new File(root, rel);
        String rootPath = root.getCanonicalPath();
        String path = f.getCanonicalPath();
        if (!path.equals(rootPath) && !path.startsWith(rootPath + File.separator)) {
            throw new IOException("路径越界: " + rel);
        }
        return f;
    }

    @Override
    public boolean exists(String rel) throws IOException {
        File f = resolve(rel);
        return f.isFile() && f.length() > 0;
    }

    @Override
    public OutputStream open(String rel) throws IOException {
        File f = resolve(rel);
        File dir = f.getParentFile();
        if (dir != null && !dir.exists() && !dir.mkdirs()) throw new IOException("无法创建目录: " + dir);
        return new FileOutputStream(f);
    }

    @Override
    public String describe() {
        return root.getAbsolutePath();
    }
}
