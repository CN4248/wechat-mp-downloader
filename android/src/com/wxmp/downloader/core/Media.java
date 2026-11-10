package com.wxmp.downloader.core;

/** 一篇文章里的一个媒体项（图片或动图）。 */
public class Media {
    public int seq;
    public String kind = "image";   // image / gif
    public String url = "";         // 原图 URL
    public String previewUrl = "";
    public String ext = "jpg";
    public int width;
    public int height;
    public boolean gif;
    public String fileId = "";
    public String source = "content";

    // 文章元数据（每项都带一份，便于渲染命名模板）
    public String title = "";
    public String postId = "";
    public String account = "";
    public String author = "";
    public String biz = "";
    public String sn = "";
    public String mid = "";
    public String idx = "";
    public long publishTime;
    public String articleUrl = "";
    public String collectionId = "";
    public String collectionTitle = "";
    public int collectionOrder;

    public void metaFrom(Article a) {
        title = a.title;
        postId = a.postId;
        account = a.account;
        author = a.author;
        biz = a.biz;
        sn = a.sn;
        mid = a.mid;
        idx = a.idx;
        publishTime = a.publishTime;
        articleUrl = a.url;
    }
}
