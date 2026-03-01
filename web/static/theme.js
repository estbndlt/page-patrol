(function () {
  var STORAGE_KEY = "page-patrol-theme";
  var mediaQuery = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  function readPreference() {
    try {
      var value = window.localStorage.getItem(STORAGE_KEY);
      if (value === "light" || value === "dark" || value === "system") {
        return value;
      }
    } catch (error) {}

    return "system";
  }

  function resolvedTheme(preference) {
    if (preference === "light" || preference === "dark") {
      return preference;
    }

    if (mediaQuery && mediaQuery.matches) {
      return "dark";
    }

    return "light";
  }

  function nextPreference(preference) {
    if (preference === "system") {
      return "light";
    }

    if (preference === "light") {
      return "dark";
    }

    return "system";
  }

  function savePreference(preference) {
    try {
      if (preference === "system") {
        window.localStorage.removeItem(STORAGE_KEY);
        return;
      }

      window.localStorage.setItem(STORAGE_KEY, preference);
    } catch (error) {}
  }

  function labelFor(preference) {
    if (preference === "light") {
      return "Theme: Light";
    }

    if (preference === "dark") {
      return "Theme: Dark";
    }

    return "Theme: Auto";
  }

  function applyTheme(preference) {
    var root = document.documentElement;
    var theme = resolvedTheme(preference);

    if (preference === "system") {
      root.removeAttribute("data-theme");
    } else {
      root.setAttribute("data-theme", preference);
    }

    root.setAttribute("data-resolved-theme", theme);
    updateButtons(preference, theme);
  }

  function updateButtons(preference, theme) {
    var buttons = document.querySelectorAll("[data-theme-cycle]");
    var next = nextPreference(preference);

    buttons.forEach(function (button) {
      button.textContent = labelFor(preference);
      button.setAttribute("data-theme-preference", preference);
      button.setAttribute("data-resolved-theme", theme);
      button.setAttribute("title", labelFor(preference));
      button.setAttribute(
        "aria-label",
        labelFor(preference) + ". Activate to switch to " + labelFor(next).replace("Theme: ", "") + "."
      );
    });
  }

  function handleThemeCycle(event) {
    var button = event.target.closest("[data-theme-cycle]");
    var currentPreference;
    var next;

    if (!button) {
      return;
    }

    currentPreference = readPreference();
    next = nextPreference(currentPreference);
    savePreference(next);
    applyTheme(next);
  }

  function bindEvents() {
    document.addEventListener("click", handleThemeCycle);

    if (!mediaQuery) {
      return;
    }

    if (mediaQuery.addEventListener) {
      mediaQuery.addEventListener("change", function () {
        if (readPreference() === "system") {
          applyTheme("system");
        }
      });
      return;
    }

    if (mediaQuery.addListener) {
      mediaQuery.addListener(function () {
        if (readPreference() === "system") {
          applyTheme("system");
        }
      });
    }
  }

  applyTheme(readPreference());

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () {
      applyTheme(readPreference());
      bindEvents();
    });
  } else {
    bindEvents();
  }
})();
