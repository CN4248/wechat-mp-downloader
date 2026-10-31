import com.wxmp.downloader.core.Article;
import com.wxmp.downloader.core.Downloader;
import com.wxmp.downloader.core.FileSink;
import com.wxmp.downloader.core.Http;
import com.wxmp.downloader.core.Media;
import com.wxmp.downloader.core.NameTemplate;
import com.wxmp.downloader.core.WxParser;

import java.io.File;
import java.util.concurrent.atomic.AtomicBoolean;

/** 在桌面 JVM 上跑与 APK 完全相同的 core 代码，验证解析与下载链路。 */
public final class SelfTest {
    public static void main(String[] args) throws Exception {
        String page = args.length > 0 ? args[0]
                : "https://mp.weixin.qq.com/s/y9op-1LgaEJ5hEYcFJyFJw";
        String out = args.length > 1 ? args[1] : "/tmp/wxmp-android-selftest";
        String tpl = args.length > 2 ? args[2] : "<文章标题>/<文章id>-<图片序号>.<扩展名>";

        System.out.println("[1] 抓取 " + page);
        byte[] data = Http.get(page);
        String html = Http.text(data);
        System.out.println("    HTML " + html.length() + " chars");

        System.out.println("[2] 解析");
        Article a = WxParser.parse(html, page);
        System.out.println("    标题=" + a.title + " 公众号=" + a.account
                + " id=" + a.postId + " medias=" + a.medias.size());
        if (a.medias.isEmpty()) throw new IllegalStateException("解析结果为空");
        for (int i = 0; i < a.medias.size(); i++) {
            Media m = a.medias.get(i);
            System.out.println("    #" + m.seq + " " + m.kind + " " + m.width + "x" + m.height
                    + " " + NameTemplate.build(m, tpl));
        }

        System.out.println("[3] 下载到 " + out);
        File root = new File(out);
        deleteTree(root);
        final int[] finish = new int[4];
        Downloader d = new Downloader(a, tpl, new FileSink(root), false,
                new AtomicBoolean(false), new Downloader.Listener() {
            @Override
            public void onLog(String message, boolean error) {
                System.out.println("    " + (error ? "! " : "· ") + message);
            }

            @Override
            public void onProgress(int done, int total) {
                System.out.println("    进度 " + done + "/" + total);
            }

            @Override
            public void onFinish(int ok, int skipped, int failed, boolean canceled) {
                finish[0] = ok;
                finish[1] = skipped;
                finish[2] = failed;
                finish[3] = canceled ? 1 : 0;
            }
        });
        d.run();
        System.out.println("    完成: ok=" + finish[0] + " skip=" + finish[1]
                + " fail=" + finish[2] + " canceled=" + finish[3]);
        if (finish[2] != 0 || finish[0] != a.medias.size()) {
            throw new IllegalStateException("下载失败数量不符");
        }
        java.util.List<File> files = new java.util.ArrayList<>();
        collect(root, files);
        if (files.size() != a.medias.size()) {
            throw new IllegalStateException("落盘文件数量 " + files.size() + " != 媒体数 " + a.medias.size());
        }
        long total = 0;
        for (File f : files) total += f.length();
        System.out.println("[4] PASS 落盘 " + files.size() + " 个文件，" + total + " 字节，目录=" + root);
    }

    private static void collect(File dir, java.util.List<File> out) {
        File[] items = dir.listFiles();
        if (items == null) return;
        for (File f : items) {
            if (f.isDirectory()) collect(f, out);
            else out.add(f);
        }
    }

    private static void deleteTree(File f) {
        if (f == null || !f.exists()) return;
        File[] cs = f.listFiles();
        if (cs != null) for (File c : cs) deleteTree(c);
        f.delete();
    }
}
