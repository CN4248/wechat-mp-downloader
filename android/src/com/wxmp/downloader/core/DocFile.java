package com.wxmp.downloader.core;

import android.content.ContentResolver;
import android.content.Context;
import android.database.Cursor;
import android.net.Uri;
import android.provider.DocumentsContract;

import java.io.IOException;

/** 轻量 SAF 文档封装，只依赖框架 API。 */
public final class DocFile {
    private final ContentResolver cr;
    private final Uri treeUri;
    private final Uri docUri;
    private final String displayName;
    private final boolean directory;
    private final long size;

    private DocFile(ContentResolver cr, Uri treeUri, Uri docUri, String name, boolean dir, long size) {
        this.cr = cr;
        this.treeUri = treeUri;
        this.docUri = docUri;
        this.displayName = name;
        this.directory = dir;
        this.size = size;
    }

    public static DocFile fromTreeUri(Context ctx, Uri tree) {
        String id = DocumentsContract.getTreeDocumentId(tree);
        Uri doc = DocumentsContract.buildDocumentUriUsingTree(tree, id);
        DocFile f = query(ctx.getContentResolver(), tree, doc);
        if (f != null) return f;
        return new DocFile(ctx.getContentResolver(), tree, doc, "", true, 0);
    }

    private static DocFile query(ContentResolver cr, Uri tree, Uri doc) {
        Cursor c = null;
        try {
            c = cr.query(doc, new String[]{
                    DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                    DocumentsContract.Document.COLUMN_MIME_TYPE,
                    DocumentsContract.Document.COLUMN_SIZE}, null, null, null);
            if (c != null && c.moveToFirst()) {
                String name = c.getString(0);
                String mime = c.getString(1);
                boolean dir = DocumentsContract.Document.MIME_TYPE_DIR.equals(mime);
                long size = c.isNull(2) ? 0 : c.getLong(2);
                return new DocFile(cr, tree, doc, name == null ? "" : name, dir, size);
            }
        } catch (Exception ignored) {
        } finally {
            if (c != null) c.close();
        }
        return null;
    }

    public String name() { return displayName; }

    public Uri uri() { return docUri; }

    public boolean isDirectory() { return directory; }

    public boolean isFile() { return !directory; }

    public long length() { return size; }

    private Uri childrenUri() {
        return DocumentsContract.buildChildDocumentsUriUsingTree(
                treeUri, DocumentsContract.getDocumentId(docUri));
    }

    public DocFile findFile(String name) {
        Cursor c = null;
        try {
            c = cr.query(childrenUri(), new String[]{
                    DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                    DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                    DocumentsContract.Document.COLUMN_MIME_TYPE,
                    DocumentsContract.Document.COLUMN_SIZE}, null, null, null);
            while (c != null && c.moveToNext()) {
                String n = c.getString(1);
                if (n != null && n.equals(name)) {
                    String id = c.getString(0);
                    String mime = c.getString(2);
                    long s = c.isNull(3) ? 0 : c.getLong(3);
                    Uri doc = DocumentsContract.buildDocumentUriUsingTree(treeUri, id);
                    return new DocFile(cr, treeUri, doc, n,
                            DocumentsContract.Document.MIME_TYPE_DIR.equals(mime), s);
                }
            }
        } catch (Exception ignored) {
        } finally {
            if (c != null) c.close();
        }
        return null;
    }

    public DocFile createDirectory(String name) throws IOException {
        Uri doc = DocumentsContract.createDocument(cr, docUri,
                DocumentsContract.Document.MIME_TYPE_DIR, name);
        if (doc == null) throw new IOException("创建目录失败: " + name);
        return new DocFile(cr, treeUri, doc, name, true, 0);
    }

    public DocFile createFile(String mime, String name) throws IOException {
        Uri doc = DocumentsContract.createDocument(cr, docUri, mime, name);
        if (doc == null) throw new IOException("创建文件失败: " + name);
        return new DocFile(cr, treeUri, doc, name, false, 0);
    }

    public boolean delete() {
        try {
            return DocumentsContract.deleteDocument(cr, docUri);
        } catch (Exception e) {
            return false;
        }
    }
}
