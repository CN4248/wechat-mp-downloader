package com.wxmp.downloader.core;

import java.io.OutputStream;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

/** 并发下载器：失败重试、跳过已存在、按真实格式修正扩展名。 */
public final class Downloader {
    public interface Listener {
        void onLog(String message, boolean error);

        void onProgress(int done, int total);

        void onFinish(int ok, int skipped, int failed, boolean canceled);
    }

    private final Article article;
    private final String template;
    private final Sink sink;
    private final boolean force;
    private final AtomicBoolean cancel;
    private final Listener listener;
    private final AtomicInteger ok = new AtomicInteger();
    private final AtomicInteger skipped = new AtomicInteger();
    private final AtomicInteger failed = new AtomicInteger();
    private final AtomicInteger done = new AtomicInteger();

    public Downloader(Article article, String template, Sink sink, boolean force,
                      AtomicBoolean cancel, Listener listener) {
        this.article = article;
        this.template = template;
        this.sink = sink;
        this.force = force;
        this.cancel = cancel;
        this.listener = listener;
    }

    public void run() {
        List<Media> medias = article.medias;
        int total = medias.size();
        if (total == 0) {
            listener.onFinish(0, 0, 0, false);
            return;
        }
        ExecutorService pool = Executors.newFixedThreadPool(3);
        CountDownLatch latch = new CountDownLatch(total);
        for (Media m : medias) {
            pool.execute(new Task(this, m, total, latch));
        }
        try {
            while (!latch.await(300, TimeUnit.MILLISECONDS)) {
                if (cancel.get()) {
                    pool.shutdownNow();
                    break;
                }
            }
            if (!cancel.get()) pool.shutdown();
            latch.await(10, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        listener.onFinish(ok.get(), skipped.get(), failed.get(), cancel.get());
    }

    private static final class Task implements Runnable {
        private final Downloader owner;
        private final Media media;
        private final int total;
        private final CountDownLatch latch;

        Task(Downloader owner, Media media, int total, CountDownLatch latch) {
            this.owner = owner;
            this.media = media;
            this.total = total;
            this.latch = latch;
        }

        @Override
        public void run() {
            try {
                owner.one(media);
            } finally {
                owner.done.incrementAndGet();
                owner.listener.onProgress(owner.done.get(), total);
                latch.countDown();
            }
        }
    }

    private void one(Media m) {
        String rel = NameTemplate.build(m, template);
        try {
            if (cancel.get()) return;
            if (!force && sink.exists(rel)) {
                skipped.incrementAndGet();
                listener.onLog("跳过已存在：" + rel, false);
                return;
            }
            byte[] data = Http.get(m.url);
            if (data.length == 0) throw new Exception("空文件");
            String actual = probeExt(data);
            if (!actual.isEmpty() && !actual.equals(m.ext)) {
                m.ext = actual;
                rel = NameTemplate.build(m, template);
            }
            if (!force && sink.exists(rel)) {
                skipped.incrementAndGet();
                listener.onLog("跳过已存在：" + rel, false);
                return;
            }
            try (OutputStream out = sink.open(rel)) {
                out.write(data);
                out.flush();
            }
            ok.incrementAndGet();
            listener.onLog("✓ " + rel + "  " + human(data.length), false);
        } catch (Exception e) {
            failed.incrementAndGet();
            listener.onLog("✗ " + rel + "  " + e.getMessage(), true);
        }
    }

    public static String probeExt(byte[] b) {
        if (b.length >= 8 && (b[0] & 0xff) == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G') return "png";
        if (b.length >= 3 && (b[0] & 0xff) == 0xff && (b[1] & 0xff) == 0xd8 && (b[2] & 0xff) == 0xff) return "jpg";
        if (b.length >= 6 && b[0] == 'G' && b[1] == 'I' && b[2] == 'F') return "gif";
        if (b.length >= 12 && b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F'
                && b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P') return "webp";
        if (b.length >= 2 && b[0] == 'B' && b[1] == 'M') return "bmp";
        return "";
    }

    public static String human(long n) {
        if (n < 1024) return n + " B";
        if (n < 1024 * 1024) return String.format(java.util.Locale.ROOT, "%.0f KB", n / 1024.0);
        return String.format(java.util.Locale.ROOT, "%.2f MB", n / 1024.0 / 1024.0);
    }
}
