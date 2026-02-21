(function () {
  var prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  var spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
  var spinnerNode = document.querySelector(".spinner");
  var spinnerIndex = 0;

  if (spinnerNode && !prefersReducedMotion) {
    setInterval(function () {
      spinnerIndex = (spinnerIndex + 1) % spinnerFrames.length;
      spinnerNode.textContent = spinnerFrames[spinnerIndex];
    }, 140);
  }

  function fallbackCopy(text) {
    var textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "absolute";
    textarea.style.left = "-9999px";
    document.body.appendChild(textarea);
    textarea.select();
    var ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (err) {
      ok = false;
    }
    document.body.removeChild(textarea);
    return ok;
  }

  var button = document.querySelector(".copy-btn");
  if (button) {
    button.addEventListener("click", function () {
      var targetId = button.getAttribute("data-copy-target");
      if (!targetId) {
        return;
      }

      var node = document.getElementById(targetId);
      if (!node) {
        return;
      }

      var text = node.textContent || "";
      var original = button.textContent;

      function flash(label) {
        button.textContent = label;
        setTimeout(function () {
          button.textContent = original;
        }, 1000);
      }

      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(
          function () {
            flash("copied");
          },
          function () {
            flash(fallbackCopy(text) ? "copied" : "failed");
          }
        );
      } else {
        flash(fallbackCopy(text) ? "copied" : "failed");
      }
    });
  }

  function monogram(name) {
    var parts = name.split(/\s+/).filter(Boolean);
    var chars = parts.slice(0, 2).map(function (p) {
      return p.charAt(0).toUpperCase();
    });
    return chars.join("") || "?";
  }

  var cards = document.querySelectorAll(".provider-card");
  cards.forEach(function (card) {
    var img = card.querySelector("img");
    if (!img) {
      return;
    }

    img.addEventListener("error", function () {
      var fallback = document.createElement("span");
      fallback.className = "fallback-logo";
      fallback.textContent = monogram(card.getAttribute("data-name") || "");
      img.replaceWith(fallback);
    });
  });
})();
