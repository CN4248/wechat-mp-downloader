/* 在首屏渲染前套用主题，避免刷新时闪白/闪黑。
   取值：auto | sun | mint | ocean | sakura | dark | midnight */
(function () {
  var KEY = "wxmp.theme.v1";
  var DARK = { dark: 1, midnight: 1 };
  var choice = "auto";
  try { choice = localStorage.getItem(KEY) || "auto"; } catch (e) {}
  var resolved = choice;
  if (choice === "auto") {
    var isDark = false;
    try { isDark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches; } catch (e) {}
    resolved = isDark ? "dark" : "mint";
  }
  var root = document.documentElement;
  root.setAttribute("data-theme", resolved);
  root.setAttribute("data-theme-choice", choice);
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", resolved === "sun" ? "#fbf5ea" : (DARK[resolved] ? "#0d1411" : "#eef4f0"));
})();
