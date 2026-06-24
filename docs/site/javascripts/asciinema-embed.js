(function () {
  function basePrefix() {
    var base = document.querySelector("base");
    var prefix = base ? base.getAttribute("href") || "/" : "/";
    if (!prefix.endsWith("/")) {
      prefix += "/";
    }
    return prefix;
  }

  function isTutorialPage() {
    return window.location.pathname.indexOf("/tutorials/") !== -1;
  }

  function castUrl(slug) {
    return basePrefix() + "asciinema/help/" + slug + ".cast";
  }

  function castPathUrl(path) {
    return basePrefix() + "asciinema/" + path;
  }

  function createKeyboardButton(playerEl) {
    var playerButton = playerEl.querySelector(".ap-kbd-button");
    if (!playerButton) {
      return null;
    }

    var button = document.createElement("button");
    button.type = "button";
    button.className = "hydra-asciinema-info__icon";
    button.setAttribute("aria-label", "Show keyboard shortcuts");
    button.title = "Show keyboard shortcuts";

    var svg = playerButton.querySelector("svg");
    if (svg) {
      button.appendChild(svg.cloneNode(true));
    } else {
      button.textContent = "?";
    }

    button.addEventListener("click", function () {
      playerButton.click();
    });

    return button;
  }

  function dispatchPlayerShortcut(playerEl, key) {
    var wrapper = playerEl.querySelector(".ap-wrapper");
    if (!wrapper) {
      return;
    }

    wrapper.focus();
    wrapper.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: key,
        bubbles: true,
      })
    );
  }

  function createFrameButton(playerEl, key, label, title, stepCount) {
    var button = document.createElement("button");
    button.type = "button";
    button.className = "hydra-asciinema-button";
    button.textContent = label;
    button.title = title;
    button.setAttribute("aria-label", title);
    var steps = Number.isFinite(stepCount) && stepCount > 0 ? Math.floor(stepCount) : 1;
    button.addEventListener("click", function () {
      for (var index = 0; index < steps; index += 1) {
        dispatchPlayerShortcut(playerEl, key);
      }
    });
    return button;
  }

  function createSeekButton(player, label, title, targetTime, variantClass) {
    var button = document.createElement("button");
    button.type = "button";
    button.className = "hydra-asciinema-button" + (variantClass ? " " + variantClass : "");
    button.textContent = label;
    button.title = title;
    button.setAttribute("aria-label", title);
    button.addEventListener("click", function () {
      player.seek(targetTime);
    });
    return button;
  }

  function createBoundaryButton(player, label, title, targetTime) {
    return createSeekButton(player, label, title, targetTime, "hydra-asciinema-button--marker");
  }

  function normalizeMarkerEntries(markers) {
    if (!Array.isArray(markers)) {
      return [];
    }

    return markers
      .map(function (marker) {
        if (Array.isArray(marker) && marker.length >= 2) {
          return {
            time: Number(marker[0]),
            label: String(marker[1]),
          };
        }
        if (marker && typeof marker === "object") {
          return {
            time: Number(marker.time),
            label: marker.label == null ? "" : String(marker.label),
          };
        }
        return null;
      })
      .filter(function (marker) {
        return marker && Number.isFinite(marker.time) && marker.label.trim() !== "";
      });
  }

  function markersFromMetadata(event) {
    if (!event) {
      return [];
    }
    return normalizeMarkerEntries(event.markers);
  }

  function durationFromMetadata(event) {
    if (!event || !Number.isFinite(Number(event.duration))) {
      return undefined;
    }
    return Number(event.duration);
  }

  function resolvePlayerTimeline(player, metadataEvent) {
    var markerResult = typeof player.getMarkers === "function"
      ? player.getMarkers()
      : markersFromMetadata(metadataEvent);
    var durationResult = typeof player.getDuration === "function"
      ? player.getDuration()
      : durationFromMetadata(metadataEvent);

    return Promise.all([Promise.resolve(markerResult), Promise.resolve(durationResult)])
      .then(function (values) {
        var markers = normalizeMarkerEntries(values[0]);
        var duration = Number(values[1]);
        if (!Number.isFinite(duration)) {
          duration = durationFromMetadata(metadataEvent);
        }
        return {
          markers: markers,
          duration: Number.isFinite(duration) ? duration : undefined,
        };
      })
      .catch(function () {
        return {
          markers: markersFromMetadata(metadataEvent),
          duration: durationFromMetadata(metadataEvent),
        };
      });
  }

  function withCurrentTime(player, callback) {
    Promise.resolve(player.getCurrentTime()).then(function (currentTime) {
      callback(currentTime || 0);
    });
  }

  function createMarkerNavigationButton(player, direction, label, title) {
    var button = document.createElement("button");
    button.type = "button";
    button.className = "hydra-asciinema-button hydra-asciinema-button--marker";
    button.textContent = label;
    button.title = title;
    button.setAttribute("aria-label", title);
    button.addEventListener("click", function () {
      player.seek({ marker: direction < 0 ? "prev" : "next" });
    });
    return button;
  }

  function formatMarkerTime(seconds) {
    var totalSeconds = Math.max(0, Math.round(seconds));
    var minutes = Math.floor(totalSeconds / 60);
    var remainingSeconds = totalSeconds % 60;
    return minutes + ":" + String(remainingSeconds).padStart(2, "0");
  }

  function setActiveMarker(buttons, markers, currentTime) {
    if (!buttons.length || !markers.length) {
      return;
    }

    var activeIndex = 0;
    for (var index = 0; index < markers.length; index += 1) {
      if (markers[index].time <= currentTime + 0.05) {
        activeIndex = index;
      } else {
        break;
      }
    }

    buttons.forEach(function (button, index) {
      var isActive = index === activeIndex;
      button.classList.toggle("is-active", isActive);
      button.setAttribute("aria-current", isActive ? "true" : "false");
    });
  }

  function installMarkerTracking(player, buttons, markers) {
    if (!player || !buttons.length || !markers.length) {
      return;
    }

    function refresh() {
      withCurrentTime(player, function (currentTime) {
        setActiveMarker(buttons, markers, currentTime || 0);
      });
    }

    ["ready", "play", "pause", "seeked", "ended", "marker"].forEach(function (eventName) {
      player.addEventListener(eventName, refresh);
    });

    setActiveMarker(buttons, markers, 0);
    window.setInterval(refresh, 250);
  }

  function ensurePlayerExtras(playerEl, player, markerData) {
    if (playerEl.dataset.extraUiInitialized === "1") {
      return;
    }

    var panel = document.createElement("div");
    panel.className = "hydra-asciinema-panel";

    var controls = document.createElement("div");
    controls.className = "hydra-asciinema-panel__controls";

    var markers = markerData && Array.isArray(markerData.markers) ? markerData.markers : [];
    var endTime = markerData && typeof markerData.duration === "number"
      ? markerData.duration
      : (markers.length > 0 ? markers[markers.length - 1].time : 0);
    var tocMarkers = markers;
    var navigableMarkers = markers;

    controls.appendChild(
      createBoundaryButton(player, "Start", "Jump to start", 0)
    );

    if (navigableMarkers.length > 0) {
      controls.appendChild(
        createMarkerNavigationButton(
          player,
          -1,
          "<< Marker",
          "Jump to previous marker"
        )
      );
    }

    controls.appendChild(
      createFrameButton(playerEl, ",", "<< Frame", "Previous 10 frames (, x10)", 10)
    );
    controls.appendChild(
      createFrameButton(playerEl, ",", "< Frame", "Previous frame (,)")
    );
    controls.appendChild(
      createFrameButton(playerEl, ".", "Frame >", "Next frame (.)")
    );
    controls.appendChild(
      createFrameButton(playerEl, ".", "Frame >>", "Next 10 frames (. x10)", 10)
    );

    if (navigableMarkers.length > 0) {
      controls.appendChild(
        createMarkerNavigationButton(
          player,
          1,
          "Marker >>",
          "Jump to next marker"
        )
      );
    }

    controls.appendChild(
      createBoundaryButton(
        player,
        "End",
        "Jump to end",
        endTime
      )
    );

    panel.appendChild(controls);

    if (tocMarkers.length > 0) {
      var toc = document.createElement("div");
      toc.className = "hydra-asciinema-toc";

      var heading = document.createElement("p");
      heading.className = "hydra-asciinema-toc__heading";
      heading.textContent = "Timeline";
      toc.appendChild(heading);

      var list = document.createElement("div");
      list.className = "hydra-asciinema-toc__list";
      var buttons = [];

      tocMarkers.forEach(function (marker) {
        var button = document.createElement("button");
        button.type = "button";
        button.className = "hydra-asciinema-toc__item";
        button.addEventListener("click", function () {
          player.seek(marker.time);
        });

        var time = document.createElement("span");
        time.className = "hydra-asciinema-toc__time";
        time.textContent = formatMarkerTime(marker.time);

        var label = document.createElement("span");
        label.className = "hydra-asciinema-toc__label";
        label.textContent = marker.label;

        button.appendChild(time);
        button.appendChild(label);
        list.appendChild(button);
        buttons.push(button);
      });

      toc.appendChild(list);
      panel.appendChild(toc);
      installMarkerTracking(player, buttons, tocMarkers);
    }

    playerEl.parentNode.insertBefore(panel, playerEl.nextSibling);
    playerEl.dataset.extraUiInitialized = "1";
  }

  function seekPlayerToEndOnLoad(playerEl, player, markerData) {
    if (playerEl.dataset.initialSeekToEndDone === "1") {
      return;
    }

    var markers = markerData && Array.isArray(markerData.markers) ? markerData.markers : [];
    var endTime = markerData && typeof markerData.duration === "number"
      ? markerData.duration
      : (markers.length > 0 ? markers[markers.length - 1].time : undefined);

    if (!Number.isFinite(endTime) || endTime < 0) {
      return;
    }

    player.seek(endTime);
    if (typeof player.pause === "function") {
      player.pause();
    }
    playerEl.dataset.initialSeekToEndDone = "1";
  }

  function ensureTutorialInfoBox(playerEl) {
    if (!isTutorialPage()) {
      return;
    }

    if (playerEl.dataset.infoBoxInitialized === "1") {
      return;
    }

    var existing = playerEl.previousElementSibling;
    if (existing && existing.classList.contains("hydra-asciinema-info")) {
      playerEl.dataset.infoBoxInitialized = "1";
      return;
    }

    var box = document.createElement("div");
    box.className = "hydra-asciinema-info";

    var heading = document.createElement("p");
    heading.className = "hydra-asciinema-info__heading";
    heading.textContent = "Keyboard Navigation";

    var text = document.createElement("p");
    text.className = "hydra-asciinema-info__text";
    text.appendChild(document.createTextNode("Use the hotkeys "));

    var zero = document.createElement("kbd");
    zero.textContent = "0";
    text.appendChild(zero);

    text.appendChild(document.createTextNode(", "));

    var comma = document.createElement("kbd");
    comma.textContent = ",";
    text.appendChild(comma);

    text.appendChild(document.createTextNode(" and "));

    var dot = document.createElement("kbd");
    dot.textContent = ".";
    text.appendChild(dot);

    text.appendChild(document.createTextNode(" or the buttons below for navigation."));

    var hint = document.createElement("p");
    hint.className = "hydra-asciinema-info__text hydra-asciinema-info__hint";
    hint.appendChild(document.createTextNode("Click the "));

    var iconButton = createKeyboardButton(playerEl);
    if (iconButton) {
      hint.appendChild(iconButton);
    } else {
      var label = document.createElement("span");
      label.className = "hydra-asciinema-info__label";
      label.textContent = "keyboard icon";
      hint.appendChild(label);
    }

    hint.appendChild(document.createTextNode(" icon in the lower-right corner for more information."));

    box.appendChild(heading);
    box.appendChild(text);
    box.appendChild(hint);
    playerEl.parentNode.insertBefore(box, playerEl);
    playerEl.dataset.infoBoxInitialized = "1";
  }

  function initPlayers() {
    if (typeof AsciinemaPlayer === "undefined") {
      return;
    }
    var players = document.querySelectorAll(".hydra-asciinema[data-cast-slug], .hydra-asciinema[data-cast-path]");

    players.forEach(function (el) {
      if (el.dataset.initialized === "1") {
        return;
      }
      var slug = el.getAttribute("data-cast-slug");
      var castPath = el.getAttribute("data-cast-path");
      if (!slug && !castPath) {
        return;
      }
      el.dataset.initialized = "1";
      var src = castPath ? castPathUrl(castPath) : castUrl(slug);
      // v3 API: create(src, containerElement, opts)
      var player = AsciinemaPlayer.create(src, el, {
        autoPlay: true,
        preload: true,
        fit: "width",
        terminalFontSize: "small",
        // Always show the timeline scrubber (playback history), not only on hover.
        controls: true,
        // Preserve every output frame when stepping with "," / "." while paused.
        minFrameTime: 0,
      });

      var metadataEvent = null;

      function renderPlayerExtras() {
        resolvePlayerTimeline(player, metadataEvent).then(function (markerData) {
          ensurePlayerExtras(el, player, markerData);
          seekPlayerToEndOnLoad(el, player, markerData);
        });
      }

      player.addEventListener("ready", renderPlayerExtras);

      player.addEventListener("metadata", function (event) {
        metadataEvent = event;
        renderPlayerExtras();
      });
    });

    if (players.length > 0) {
      ensureTutorialInfoBox(players[0]);
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initPlayers);
  } else {
    initPlayers();
  }

  document$.subscribe(function () {
    initPlayers();
  });
})();
