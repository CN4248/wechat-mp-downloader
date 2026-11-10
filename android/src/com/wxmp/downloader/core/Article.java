package com.wxmp.downloader.core;

import java.util.ArrayList;
import java.util.List;

public class Article {
    public String url = "";
    public String title = "";
    public String account = "";
    public String author = "";
    public String postId = "";
    public String biz = "";
    public String sn = "";
    public String mid = "";
    public String idx = "";
    public long publishTime;
    public String coverUrl = "";
    public final List<Media> medias = new ArrayList<>();
    public final List<String> warnings = new ArrayList<>();
}
