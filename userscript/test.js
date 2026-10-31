// Node 自测：不依赖浏览器，验证解析、原图 URL、去重与命名规则。
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const u = require("./wxmp.user.js");

const html = fs.readFileSync(path.join(__dirname, "..", "testdata", "article-fixture.html"), "utf8");
const a = u.parsePageHTML(html, "https://mp.weixin.qq.com/s/fixtureslug123");
assert.equal(a.title, "测试文章标题", "title");
assert.equal(a.account, "测试号", "account");
assert.equal(a.post_id, "fixtureslug123", "post_id 取 URL slug");
assert.equal(a.medias.length, 3, "去重后 3 项");
assert.ok(a.medias.every(m => !m.url.includes("watermark")), "不含水印图");
assert.ok(a.medias.every(m => /\/0\?wx_fmt=/.test(m.url)), "全部转 /0 原图");
const gif = a.medias.find(m => m.kind === "gif");
assert.ok(gif && gif.ext === "gif", "识别 GIF");
assert.equal(u.buildRelPath(Object.assign({}, a, a.medias[0]), "<文章标题>/<文章id>-<图片序号>.<扩展名>"), "测试文章标题/fixtureslug123-1.png");
assert.equal(u.buildRelPath(Object.assign({}, a, a.medias[0]), u.DEFAULT_TEMPLATE), "测试文章标题-fixtureslug123-1.png");
assert.equal(u.buildRelPath(Object.assign({}, a, a.medias[0]), "<order2>_<kind>.<ext>"), "01_image.png");
assert.throws(() => u.validateTemplate("<title"), /未闭合|变量/);
const hash = "leqgpMdQQJgA9PRGpwwuicooZ7ym8spA9R7ntHQnqp5j0OQRnCHoWbTl8xtLdISJ2POBVprRVhC8OLocFl3TGZSFMQyaZx8tjKShopl2S5fs";
const variant = '<div id="js_content"><img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/' + hash + '/640?wx_fmt=jpeg" data-type="jpeg"></div>' +
  "<script>picture_page_info_list: [{ cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/" + hash + "/0?wx_fmt=jpeg', width: '849'*1, height: '1200'*1 }];</script>";
const b = u.parsePageHTML(variant, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g");
assert.equal(b.post_id, "tC1hauHuVvqRusojhhXe5g", "slug post_id");
assert.equal(b.medias.length, 1, "CDN 前缀变体去重");
const mh1 = "a".repeat(64), mh2 = "b".repeat(64), mh3 = "c".repeat(64), mh4 = "d".repeat(64);
const mirrored = '<div id="js_content"><img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/' + mh1 + '/640?wx_fmt=jpeg" data-type="jpeg" data-w="849" data-ratio="1.41">' +
  '<img data-src="https://mmbiz.qpic.cn/mmbiz_png/' + mh2 + '/640?wx_fmt=png" data-type="png" data-w="900" data-ratio="1"></div>' +
  "<script>picture_page_info_list: [{ cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/" + mh3 + "/0?wx_fmt=jpeg', width: '849'*1, height: '1197'*1 }," +
  " { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_png/" + mh4 + "/0?wx_fmt=png', width: '900'*1, height: '900'*1 }];</script>";
const c = u.parsePageHTML(mirrored, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g");
assert.equal(c.medias.length, 2, "镜像图集去重");
assert.ok(c.warnings.some(w => w.includes("重复")), "有重复提示");
const pfx = "42OhnrM9iaDRaYFbBbF96SwEaDqemF70L";
const pv = '<div id="js_content"><img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/' + pfx + 'fwPd0ZP8sB9Rnsia9JxTg5z1E99Rt0Mnsk68M83wxKxNGicSk9pZmMcQ/640?wx_fmt=png" data-type="png"></div>' +
  "<script>picture_page_info_list: [{ cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_png/" + pfx + "oqPojmI5Wa9ck/0?wx_fmt=png', width: '934'*1, height: '897'*1 }];</script>";
const d = u.parsePageHTML(pv, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g");
assert.equal(d.medias.length, 2, "共享前缀的不同图片不合并");
console.log("USERSCRIPT TESTS PASS", a.medias.map(m => m.ext).join(","));
