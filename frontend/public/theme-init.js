// Anti-FOUC theme bootstrap: applies the stored (or system) theme before the
// SPA mounts. Kept as an external file so the CSP can stay script-src 'self'.
(function () {
  try {
    var d = document.documentElement;
    var K = "theme";
    function apply() {
      var t = localStorage.getItem(K);
      if (t === "dark") {
        d.classList.add("dark");
        return;
      }
      if (t === "light") {
        d.classList.remove("dark");
        return;
      }
      window.matchMedia("(prefers-color-scheme: dark)").matches
        ? d.classList.add("dark")
        : d.classList.remove("dark");
    }
    apply();
    window
      .matchMedia("(prefers-color-scheme: dark)")
      .addEventListener("change", function () {
        if (!localStorage.getItem(K)) apply();
      });
  } catch (e) {}
})();
