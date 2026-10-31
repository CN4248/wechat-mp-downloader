package com.wxmp.downloader;

import android.app.Activity;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Bundle;
import android.os.Environment;
import android.os.Handler;
import android.os.Message;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import com.wxmp.downloader.core.Album;
import com.wxmp.downloader.core.AlbumArticle;
import com.wxmp.downloader.core.AlbumParser;
import com.wxmp.downloader.core.Article;
import com.wxmp.downloader.core.Downloader;
import com.wxmp.downloader.core.FileSink;
import com.wxmp.downloader.core.Http;
import com.wxmp.downloader.core.Media;
import com.wxmp.downloader.core.NameTemplate;
import com.wxmp.downloader.core.SafSink;
import com.wxmp.downloader.core.Sink;
import com.wxmp.downloader.core.WxParser;

import java.io.File;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicBoolean;

public class MainActivity extends Activity {
    private static final int REQ_DIR = 1001;
    private static final int GREEN = Color.rgb(7, 193, 96);
    private static final int INK = Color.rgb(22, 48, 43);
    private static final int MUTED = Color.rgb(110, 130, 124);

    private static final int MSG_LOG = 1;
    private static final int MSG_PROGRESS = 2;
    private static final int MSG_FINISH = 3;
    private static final int MSG_PARSE_OK = 4;
    private static final int MSG_PARSE_ERR = 5;
    private static final int MSG_PARSE_END = 6;
    private static final int MSG_ALBUM_OK = 7;

    private static final int ACT_PARSE = 1;
    private static final int ACT_DOWNLOAD = 2;
    private static final int ACT_CANCEL = 3;
    private static final int ACT_DIR = 4;

    private EditText urlInput;
    private EditText tplInput;
    private EditText collTplInput;
    private TextView dirText;
    private TextView summaryText;
    private TextView statusText;
    private TextView logText;
    private ProgressBar bar;
    private Button parseBtn;
    private Button downloadBtn;
    private Button cancelBtn;
    private CheckBox coverBox;
    private CheckBox forceBox;

    private final ExecutorService io = Executors.newSingleThreadExecutor();
    private final AtomicBoolean cancel = new AtomicBoolean(false);
    private Handler ui;
    private Article article;
    private Album album;
    private Sink sink;
    private boolean busy;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        ui = new Handler(new UiCallback(this));
        buildUi();
        loadPrefs();
        importShared(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        importShared(intent);
    }

    /** 接收微信“分享到其他应用”发来的链接。 */
    private void importShared(Intent in) {
        if (in == null) return;
        String text = in.getStringExtra(Intent.EXTRA_TEXT);
        if (text == null && Intent.ACTION_VIEW.equals(in.getAction()) && in.getData() != null) {
            text = in.getData().toString();
        }
        if (text == null) return;
        Matcher m = Pattern.compile("https?://[^\\s]+").matcher(text);
        if (m.find()) {
            urlInput.setText(m.group());
            log("已从分享导入链接：" + m.group(), false);
        }
    }

    /* ---------------- UI ---------------- */

    private int dp(float v) {
        return Math.round(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v,
                getResources().getDisplayMetrics()));
    }

    private TextView label(String text, float size, int color, boolean bold) {
        TextView tv = new TextView(this);
        tv.setText(text);
        tv.setTextSize(size);
        tv.setTextColor(color);
        if (bold) tv.setTypeface(Typeface.DEFAULT_BOLD);
        return tv;
    }

    private View space(int dp) {
        View v = new View(this);
        v.setLayoutParams(new LinearLayout.LayoutParams(1, dp(dp)));
        return v;
    }

    private View spaceH(int dp) {
        View v = new View(this);
        v.setLayoutParams(new LinearLayout.LayoutParams(dp(dp), 1));
        return v;
    }

    private void buildUi() {
        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(Color.rgb(246, 251, 248));
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(18), dp(22), dp(18), dp(28));
        scroll.addView(root);

        LinearLayout titleRow = new LinearLayout(this);
        titleRow.setOrientation(LinearLayout.HORIZONTAL);
        titleRow.setGravity(Gravity.CENTER_VERTICAL);
        TextView logo = label("微", 15, Color.WHITE, true);
        logo.setGravity(Gravity.CENTER);
        logo.setBackgroundColor(GREEN);
        LinearLayout.LayoutParams logoLp = new LinearLayout.LayoutParams(dp(34), dp(34));
        logoLp.setMargins(0, 0, dp(10), 0);
        titleRow.addView(logo, logoLp);
        LinearLayout titleCol = new LinearLayout(this);
        titleCol.setOrientation(LinearLayout.VERTICAL);
        titleCol.addView(label("公众号图片下载器", 20, INK, true));
        titleCol.addView(label("原图 · 动图 · 自定义目录与重命名", 11, MUTED, false));
        titleRow.addView(titleCol);
        root.addView(titleRow);

        root.addView(space(18));
        root.addView(label("文章链接", 12, MUTED, true));
        urlInput = new EditText(this);
        urlInput.setHint("https://mp.weixin.qq.com/s/...");
        urlInput.setTextSize(13);
        urlInput.setSingleLine(false);
        urlInput.setMinLines(2);
        urlInput.setMaxLines(3);
        urlInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        root.addView(urlInput);

        root.addView(space(12));
        LinearLayout dirRow = new LinearLayout(this);
        dirRow.setOrientation(LinearLayout.HORIZONTAL);
        dirRow.setGravity(Gravity.CENTER_VERTICAL);
        dirText = label("", 11, MUTED, false);
        dirText.setSingleLine(true);
        dirRow.addView(dirText, new LinearLayout.LayoutParams(0,
                ViewGroup.LayoutParams.WRAP_CONTENT, 1f));
        Button choose = new Button(this);
        choose.setText("选择目录");
        choose.setTextSize(11);
        choose.setAllCaps(false);
        choose.setOnClickListener(new Click(this, ACT_DIR));
        dirRow.addView(choose, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        root.addView(dirRow);

        root.addView(space(14));
        root.addView(label("重命名规则（/ 可创建子文件夹）", 12, MUTED, true));
        tplInput = new EditText(this);
        tplInput.setTextColor(INK);
        tplInput.setTextSize(12);
        tplInput.setTypeface(Typeface.MONOSPACE);
        tplInput.setSingleLine(true);
        tplInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        root.addView(tplInput);
        root.addView(label("<title> <post_id> <order> <ext> <account> <date> … 也可用中文：<文章标题>/<文章id>-<图片序号>.<扩展名>",
                10, MUTED, false));

        root.addView(space(8));
        root.addView(label("合集重命名规则（勾选/粘贴合集链接时生效）", 12, MUTED, true));
        collTplInput = new EditText(this);
        collTplInput.setTextColor(INK);
        collTplInput.setTextSize(12);
        collTplInput.setTypeface(Typeface.MONOSPACE);
        collTplInput.setSingleLine(true);
        collTplInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        root.addView(collTplInput);
        root.addView(label("额外变量：<collection_title>/<合集标题>、<collection_id>/<合集ID>、<collection_order>/<合集内文章序号>",
                10, MUTED, false));

        LinearLayout optRow = new LinearLayout(this);
        optRow.setOrientation(LinearLayout.HORIZONTAL);
        coverBox = new CheckBox(this);
        coverBox.setText("附带封面");
        coverBox.setTextSize(11);
        forceBox = new CheckBox(this);
        forceBox.setText("覆盖已有");
        forceBox.setTextSize(11);
        optRow.addView(coverBox);
        optRow.addView(spaceH(14));
        optRow.addView(forceBox);
        root.addView(optRow);

        LinearLayout btnRow = new LinearLayout(this);
        btnRow.setOrientation(LinearLayout.HORIZONTAL);
        parseBtn = new Button(this);
        parseBtn.setText("解析文章");
        parseBtn.setAllCaps(false);
        parseBtn.setTextSize(12);
        parseBtn.setOnClickListener(new Click(this, ACT_PARSE));
        downloadBtn = new Button(this);
        downloadBtn.setText("开始下载");
        downloadBtn.setAllCaps(false);
        downloadBtn.setTextColor(Color.WHITE);
        downloadBtn.setBackgroundColor(GREEN);
        downloadBtn.setTextSize(12);
        downloadBtn.setEnabled(false);
        downloadBtn.setOnClickListener(new Click(this, ACT_DOWNLOAD));
        cancelBtn = new Button(this);
        cancelBtn.setText("取消");
        cancelBtn.setAllCaps(false);
        cancelBtn.setTextSize(12);
        cancelBtn.setEnabled(false);
        cancelBtn.setOnClickListener(new Click(this, ACT_CANCEL));
        btnRow.addView(parseBtn, new LinearLayout.LayoutParams(0, dp(44), 1f));
        btnRow.addView(spaceH(8));
        btnRow.addView(downloadBtn, new LinearLayout.LayoutParams(0, dp(44), 1.4f));
        btnRow.addView(spaceH(8));
        btnRow.addView(cancelBtn, new LinearLayout.LayoutParams(0, dp(44), 0.7f));
        root.addView(space(10));
        root.addView(btnRow);

        bar = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        bar.setMax(100);
        bar.setProgress(0);
        root.addView(bar, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, dp(6)));
        statusText = label("待解析", 11, MUTED, false);
        root.addView(statusText);

        summaryText = label("", 12, INK, false);
        summaryText.setPadding(0, dp(6), 0, dp(4));
        root.addView(summaryText);

        ScrollView logScroll = new ScrollView(this);
        logText = new TextView(this);
        logText.setTextSize(10.5f);
        logText.setTypeface(Typeface.MONOSPACE);
        logText.setTextColor(Color.rgb(200, 235, 214));
        logText.setTextIsSelectable(true);
        logText.setPadding(dp(9), dp(8), dp(9), dp(8));
        logScroll.setBackgroundColor(Color.rgb(16, 32, 27));
        logScroll.addView(logText);
        root.addView(logScroll, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, dp(190)));

        root.addView(space(8));
        root.addView(label("只下载公开文章内容；遇到“环境异常”请在浏览器中打开一次文章后再试。",
                10, MUTED, false));

        setContentView(scroll);
    }

    /* ---------------- 配置持久化 ---------------- */

    private SharedPreferences prefs() {
        return getSharedPreferences("wxmp", MODE_PRIVATE);
    }

    private void loadPrefs() {
        SharedPreferences p = prefs();
        urlInput.setText(p.getString("url", ""));
        tplInput.setText(p.getString("template", NameTemplate.DEFAULT));
        collTplInput.setText(p.getString("collection_template", NameTemplate.DEFAULT_COLLECTION));
        coverBox.setChecked(p.getBoolean("cover", false));
        forceBox.setChecked(p.getBoolean("force", false));
        String tree = p.getString("tree", "");
        if (!tree.isEmpty()) {
            try {
                sink = new SafSink(this, Uri.parse(tree));
            } catch (Exception e) {
                sink = null;
            }
        }
        if (sink == null) sink = new FileSink(defaultDir());
        dirText.setText(sink.describe());
    }

    private void savePrefs() {
        prefs().edit()
                .putString("url", urlInput.getText().toString().trim())
                .putString("template", tplInput.getText().toString())
                .putString("collection_template", collTplInput.getText().toString())
                .putBoolean("cover", coverBox.isChecked())
                .putBoolean("force", forceBox.isChecked())
                .apply();
    }

    private File defaultDir() {
        File base = getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS);
        if (base == null) base = getFilesDir();
        return new File(base, "wxmp");
    }

    private void chooseDir() {
        Intent i = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE);
        i.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION
                | Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        try {
            startActivityForResult(i, REQ_DIR);
        } catch (Exception e) {
            toast("系统不支持选择目录，将保存到应用目录");
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode != REQ_DIR || resultCode != RESULT_OK || data == null || data.getData() == null) {
            return;
        }
        Uri uri = data.getData();
        try {
            getContentResolver().takePersistableUriPermission(uri,
                    Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            sink = new SafSink(this, uri);
            prefs().edit().putString("tree", uri.toString()).apply();
            dirText.setText(sink.describe());
            log("保存目录已切换", false);
        } catch (Exception e) {
            toast("目录授权失败：" + e.getMessage());
        }
    }

    /* ---------------- 解析 / 下载 ---------------- */

    private void startParse() {
        if (busy) return;
        final String url = urlInput.getText().toString().trim();
        if (url.isEmpty()) {
            toast("请先粘贴文章链接");
            return;
        }
        try {
            NameTemplate.validate(tplInput.getText().toString());
            NameTemplate.validate(collTplInput.getText().toString());
        } catch (Exception e) {
            toast(e.getMessage());
            return;
        }
        album = null;
        busy = true;
        setButtons(false);
        logText.setText("");
        summaryText.setText("");
        statusText.setText("正在读取文章…");
        log("GET " + url, false);
        savePrefs();
        io.execute(new ParseTask(this, url));
    }

    private void startDownload() {
        if (busy) return;
        if (album != null && !album.articles.isEmpty()) {
            startAlbumDownload();
            return;
        }
        if (article == null) return;
        try {
            NameTemplate.validate(tplInput.getText().toString());
        } catch (Exception e) {
            toast(e.getMessage());
            return;
        }
        final Article copy = new Article();
        copy.title = article.title;
        copy.account = article.account;
        copy.postId = article.postId;
        copy.author = article.author;
        copy.biz = article.biz;
        copy.sn = article.sn;
        copy.mid = article.mid;
        copy.idx = article.idx;
        copy.publishTime = article.publishTime;
        copy.url = article.url;

        List<Media> list = new ArrayList<>(article.medias);
        if (coverBox.isChecked() && article.coverUrl != null && !article.coverUrl.isEmpty()) {
            Media c = new Media();
            c.kind = "image";
            c.metaFrom(article);
            c.url = WxParser.originalUrl(article.coverUrl, "jpg");
            c.ext = "jpg";
            c.seq = 0;
            c.source = "cover";
            list.add(0, c);
        }
        int seq = 1;
        for (Media m : list) {
            m.seq = seq++;
            copy.medias.add(m);
        }

        busy = true;
        cancel.set(false);
        setButtons(false);
        cancelBtn.setEnabled(true);
        bar.setMax(list.size());
        bar.setProgress(0);
        savePrefs();
        log("开始下载 " + list.size() + " 项 → " + sink.describe(), false);
        new Thread(new DownloadTask(this, copy, tplInput.getText().toString(), forceBox.isChecked())).start();
    }

    private void startAlbumDownload() {
        try {
            NameTemplate.validate(collTplInput.getText().toString());
        } catch (Exception e) {
            toast(e.getMessage());
            return;
        }
        busy = true;
        cancel.set(false);
        setButtons(false);
        cancelBtn.setEnabled(true);
        bar.setMax(album.articles.size());
        bar.setProgress(0);
        savePrefs();
        log("开始下载合集《" + album.title + "》共 " + album.articles.size() + " 篇 → " + sink.describe(), false);
        new Thread(new AlbumDownloadTask(this, album, collTplInput.getText().toString(),
                forceBox.isChecked())).start();
    }

    private void onProgress(int done, int total) {
        bar.setMax(total);
        bar.setProgress(done);
        statusText.setText("进度 " + done + "/" + total);
    }

    private void onFinish(int ok, int skipped, int failed, boolean canceled) {
        busy = false;
        cancelBtn.setEnabled(false);
        parseBtn.setEnabled(true);
        downloadBtn.setEnabled(true);
        statusText.setText((canceled ? "已取消：" : "完成：") + ok + " 成功 · "
                + skipped + " 跳过 · " + failed + " 失败");
        log("结束：" + ok + " 成功，" + skipped + " 跳过，" + failed + " 失败", failed > 0);
    }

    private void setButtons(boolean enabled) {
        parseBtn.setEnabled(enabled);
        downloadBtn.setEnabled(enabled && article != null && !article.medias.isEmpty());
        if (enabled) cancelBtn.setEnabled(false);
    }

    private void log(String msg, boolean error) {
        logText.append((error ? "! " : "· ") + msg + "\n");
        if (logText.getLayout() != null) {
            logText.scrollTo(0, logText.getLayout().getLineTop(logText.getLineCount()));
        }
    }

    private void toast(String msg) {
        Toast.makeText(this, msg, Toast.LENGTH_SHORT).show();
    }

    @Override
    protected void onPause() {
        savePrefs();
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        cancel.set(true);
        io.shutdownNow();
        super.onDestroy();
    }

    /* ---------------- 无匿名类的辅助实现 ---------------- */

    private static final class Click implements View.OnClickListener {
        private final MainActivity a;
        private final int action;

        Click(MainActivity a, int action) {
            this.a = a;
            this.action = action;
        }

        @Override
        public void onClick(View v) {
            switch (action) {
                case ACT_PARSE: a.startParse(); break;
                case ACT_DOWNLOAD: a.startDownload(); break;
                case ACT_CANCEL: a.cancel.set(true); a.statusText.setText("正在取消…"); break;
                case ACT_DIR: a.chooseDir(); break;
                default: break;
            }
        }
    }

    private static final class AlbumDownloadTask implements Runnable {
        private final MainActivity a;
        private final Album album;
        private final String tpl;
        private final boolean force;

        AlbumDownloadTask(MainActivity a, Album album, String tpl, boolean force) {
            this.a = a;
            this.album = album;
            this.tpl = tpl;
            this.force = force;
        }

        @Override
        public void run() {
            AlbumListener listener = new AlbumListener(a);
            int total = album.articles.size();
            for (int i = 0; i < total; i++) {
                if (a.cancel.get()) break;
                AlbumArticle aa = album.articles.get(i);
                listener.label = "[" + (i + 1) + "/" + total + "] #" + aa.order + " " + aa.title;
                Message.obtain(a.ui, MSG_LOG, 0, 0, "第 " + (i + 1) + "/" + total + " 篇：" + aa.title).sendToTarget();
                Message.obtain(a.ui, MSG_PROGRESS, i, total).sendToTarget();
                try {
                    byte[] data = Http.get(aa.url);
                    Article art = WxParser.parse(Http.text(data), aa.url);
                    if (art.publishTime == 0) art.publishTime = aa.publishTime;
                    for (Media m : art.medias) {
                        m.collectionId = album.id;
                        m.collectionTitle = album.title;
                        m.collectionOrder = aa.order;
                        if (m.title.isEmpty()) m.title = art.title;
                    }
                    if (art.medias.isEmpty()) {
                        listener.failed++;
                        Message.obtain(a.ui, MSG_LOG, 1, 0, aa.title + "：没有解析到图片").sendToTarget();
                        continue;
                    }
                    new Downloader(art, tpl, a.sink, force, a.cancel, listener).run();
                } catch (Exception e) {
                    listener.failed++;
                    Message.obtain(a.ui, MSG_LOG, 1, 0,
                            aa.title + "：" + (e.getMessage() == null ? "失败" : e.getMessage())).sendToTarget();
                }
                Message.obtain(a.ui, MSG_PROGRESS, i + 1, total).sendToTarget();
            }
            Bundle b = new Bundle();
            b.putInt("ok", listener.ok);
            b.putInt("skipped", listener.skipped);
            b.putInt("failed", listener.failed);
            b.putBoolean("canceled", a.cancel.get());
            Message msg = Message.obtain(a.ui, MSG_FINISH);
            msg.setData(b);
            msg.sendToTarget();
        }
    }

    private static final class AlbumListener implements Downloader.Listener {
        private final MainActivity a;
        String label = "";
        int ok, skipped, failed;

        AlbumListener(MainActivity a) {
            this.a = a;
        }

        @Override
        public void onLog(String message, boolean error) {
            Message.obtain(a.ui, MSG_LOG, error ? 1 : 0, 0, label + " " + message).sendToTarget();
        }

        @Override
        public void onProgress(int done, int total) {
            Message.obtain(a.ui, MSG_PROGRESS, done, total).sendToTarget();
        }

        @Override
        public void onFinish(int o, int s, int f, boolean canceled) {
            ok += o;
            skipped += s;
            failed += f;
            Message.obtain(a.ui, MSG_LOG, f > 0 ? 1 : 0, 0,
                    label + " 完成：" + o + " 成功 · " + s + " 跳过 · " + f + " 失败").sendToTarget();
        }
    }

    private static final class ParseTask implements Runnable {
        private final MainActivity a;
        private final String url;

        ParseTask(MainActivity a, String url) {
            this.a = a;
            this.url = url;
        }

        @Override
        public void run() {
            try {
                if (AlbumParser.isAlbum(url)) {
                    Album al = AlbumParser.fetch(url);
                    Message.obtain(a.ui, MSG_ALBUM_OK, al).sendToTarget();
                } else {
                    byte[] data = Http.get(url);
                    Article art = WxParser.parse(Http.text(data), url);
                    Message.obtain(a.ui, MSG_PARSE_OK, art).sendToTarget();
                }
            } catch (Exception e) {
                Message.obtain(a.ui, MSG_PARSE_ERR, e.getMessage() == null ? "解析失败" : e.getMessage())
                        .sendToTarget();
            } finally {
                a.ui.sendEmptyMessage(MSG_PARSE_END);
            }
        }
    }

    private static final class DownloadTask implements Runnable {
        private final MainActivity a;
        private final Article copy;
        private final String tpl;
        private final boolean force;

        DownloadTask(MainActivity a, Article copy, String tpl, boolean force) {
            this.a = a;
            this.copy = copy;
            this.tpl = tpl;
            this.force = force;
        }

        @Override
        public void run() {
            new Downloader(copy, tpl, a.sink, force, a.cancel, new UiListener(a)).run();
        }
    }

    private static final class UiListener implements Downloader.Listener {
        private final MainActivity a;

        UiListener(MainActivity a) {
            this.a = a;
        }

        @Override
        public void onLog(String message, boolean error) {
            Message.obtain(a.ui, MSG_LOG, error ? 1 : 0, 0, message).sendToTarget();
        }

        @Override
        public void onProgress(int done, int total) {
            Message.obtain(a.ui, MSG_PROGRESS, done, total).sendToTarget();
        }

        @Override
        public void onFinish(int ok, int skipped, int failed, boolean canceled) {
            Bundle b = new Bundle();
            b.putInt("ok", ok);
            b.putInt("skipped", skipped);
            b.putInt("failed", failed);
            b.putBoolean("canceled", canceled);
            Message msg = Message.obtain(a.ui, MSG_FINISH);
            msg.setData(b);
            msg.sendToTarget();
        }
    }

    private static final class UiCallback implements Handler.Callback {
        private final MainActivity a;

        UiCallback(MainActivity a) {
            this.a = a;
        }

        @Override
        public boolean handleMessage(Message msg) {
            switch (msg.what) {
                case MSG_LOG:
                    a.log(String.valueOf(msg.obj), msg.arg1 == 1);
                    return true;
                case MSG_PROGRESS:
                    a.onProgress(msg.arg1, msg.arg2);
                    return true;
                case MSG_FINISH:
                    Bundle b = msg.getData();
                    a.onFinish(b.getInt("ok"), b.getInt("skipped"), b.getInt("failed"),
                            b.getBoolean("canceled"));
                    return true;
                case MSG_PARSE_OK:
                    Article art = (Article) msg.obj;
                    a.article = art;
                    a.summaryText.setText(art.title + "\n" + art.account + "  ·  id " + art.postId
                            + "  ·  " + art.medias.size() + " 项");
                    for (String w : art.warnings) a.log("! " + w, true);
                    a.log("解析到 " + art.medias.size() + " 项媒体", false);
                    if (art.medias.isEmpty()) {
                        a.statusText.setText("没有找到图片");
                    } else {
                        a.log("示例文件名：" + NameTemplate.build(art.medias.get(0),
                                a.tplInput.getText().toString()), false);
                        a.statusText.setText("解析完成，可以下载");
                    }
                    return true;
                case MSG_ALBUM_OK:
                    Album al = (Album) msg.obj;
                    a.album = al;
                    a.article = null;
                    a.summaryText.setText(al.title + "\n" + al.account + "  ·  合集 id " + al.id
                            + "  ·  " + al.articles.size() + " 篇");
                    for (String w : al.warnings) a.log("! " + w, true);
                    a.log("解析到合集文章 " + al.articles.size() + " 篇", false);
                    if (al.articles.isEmpty()) {
                        a.statusText.setText("合集里没有文章");
                    } else {
                        a.statusText.setText("合集已就绪，可以批量下载");
                    }
                    return true;
                case MSG_PARSE_ERR:
                    a.log("解析失败：" + msg.obj, true);
                    a.statusText.setText("解析失败");
                    return true;
                case MSG_PARSE_END:
                    a.busy = false;
                    a.setButtons(true);
                    a.downloadBtn.setEnabled((a.album != null && !a.album.articles.isEmpty())
                            || (a.article != null && !a.article.medias.isEmpty()));
                    return true;
                default:
                    return false;
            }
        }
    }
}
