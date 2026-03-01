(function () {
  var previousFeedEntries = [];
  var shouldRestoreReadFocus = false;

  function readPanel() {
    return document.getElementById("read-panel");
  }

  function feedShell() {
    return document.getElementById("feed-shell");
  }

  function feedStatus() {
    return document.getElementById("feed-status");
  }

  function feedEntries() {
    var shell = feedShell();

    if (!shell) {
      return [];
    }

    return Array.prototype.slice
      .call(shell.querySelectorAll(".feed li"))
      .filter(function (item) {
        return !item.classList.contains("feed-empty");
      })
      .map(function (item) {
        return item.textContent.replace(/\s+/g, " ").trim();
      });
  }

  function newEntryCount(previous, next) {
    var seen = Object.create(null);
    var count = 0;

    previous.forEach(function (entry) {
      seen[entry] = (seen[entry] || 0) + 1;
    });

    next.forEach(function (entry) {
      if (seen[entry]) {
        seen[entry] -= 1;
        return;
      }

      count += 1;
    });

    return count;
  }

  function announceFeedUpdate(message) {
    var status = feedStatus();

    if (!status) {
      return;
    }

    status.textContent = "";
    window.setTimeout(function () {
      status.textContent = message;
    }, 30);
  }

  function syncFeedState(announce) {
    var nextEntries = feedEntries();
    var count;

    if (!announce) {
      previousFeedEntries = nextEntries;
      return;
    }

    count = newEntryCount(previousFeedEntries, nextEntries);

    if (count > 0) {
      announceFeedUpdate(count + " new activity " + (count === 1 ? "item" : "items") + " loaded.");
    }

    previousFeedEntries = nextEntries;
  }

  function restoreReadFocus() {
    var panel = readPanel();
    var button;

    if (!shouldRestoreReadFocus || !panel) {
      return;
    }

    shouldRestoreReadFocus = false;
    button = panel.querySelector("button");

    if (button) {
      button.focus();
    }
  }

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

    var shell = feedShell();
    if (!shell) {
      return;
    }

    var stream = new EventSource("/feed/events");
    stream.addEventListener("activity", function () {
      refreshFeed();
    });
    stream.onerror = function () {};
  }

  function handleSubmit(event) {
    var form = event.target;

    if (!form || !form.closest || !form.closest("#read-panel")) {
      return;
    }

    shouldRestoreReadFocus = true;
  }

  function handleAfterSwap(event) {
    var detail = event.detail || {};
    var target = detail.target;

    if (!target) {
      return;
    }

    if (target.id === "feed-shell") {
      syncFeedState(true);
      return;
    }

    if (target.id === "read-panel") {
      restoreReadFocus();
    }
  }

  function init() {
    syncFeedState(false);
    initFeedStream();

    if (window.htmx) {
      document.body.addEventListener("htmx:afterSwap", handleAfterSwap);
    }

    document.body.addEventListener("submit", handleSubmit);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
    return;
  }

  init();
})();
