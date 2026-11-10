package com.wxmp.downloader.core;

import java.util.ArrayList;
import java.util.List;

/** 公众号文章合集（appmsgalbum）。 */
public class Album {
    public String url = "";
    public String id = "";
    public String title = "";
    public String account = "";
    public String description = "";
    public int articleCount;
    public final List<AlbumArticle> articles = new ArrayList<>();
    public final List<String> warnings = new ArrayList<>();
}
