(function () {
  function refreshFeed() {
    if (!window.htmx) {
      return;
    }

    window.htmx.ajax("GET", "/feed/fragment", {
      target: "#feed-shell",
      swap: "innerHTML"
    });
  }

  function initFeedStream() {
    if (!window.EventSource) {
      return;
    }

    var feedShell = document.getElementById("feed-shell");
    if (!feedShell) {
      return;
    }

    var stream = new EventSource("/feed/events");
    stream.addEventListener("activity", function () {
      refreshFeed();
    });
    stream.onerror = function () {};
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initFeedStream);
    return;
  }

  initFeedStream();
})();
