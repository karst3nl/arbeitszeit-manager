/* SPDX-License-Identifier: AGPL-3.0-only */
(function () {
  'use strict';

  var currentElement = null;
  var clockOffset = 0;

  function fmt(seconds) {
    var s = Math.max(0, Math.floor(seconds));
    var h = Math.floor(s / 3600);
    var m = Math.floor((s % 3600) / 60);
    var sec = s % 60;
    return [h, m, sec].map(function (v) {
      return String(v).padStart(2, '0');
    }).join(':');
  }

  function signed(seconds) {
    var s = Math.floor(seconds);
    return (s < 0 ? '-' : '+') + fmt(Math.abs(s));
  }

  function percent(delta, planned) {
    if (!planned) return '+0.0 %';
    var v = delta / planned * 100;
    return (v < 0 ? '' : '+') + v.toFixed(1) + ' %';
  }

  function tick() {
    var el = document.getElementById('timer-display');
    if (!el) {
      currentElement = null;
      return;
    }

    // The server timestamp travels with every HTMX fragment. Recompute the
    // offset only when a new fragment was swapped in, not on every second.
    if (el !== currentElement) {
      currentElement = el;
      var serverNow = Number(el.dataset.now || 0);
      clockOffset = serverNow ? serverNow - Date.now() : 0;
    }

    var base = Number(el.dataset.elapsed || 0);
    var elapsed = base;
    if (el.dataset.state === 'running') {
      var since = Number(el.dataset.since || 0);
      var serverSeconds = (Date.now() + clockOffset) / 1000;
      if (since) elapsed += Math.max(0, serverSeconds - since);
    }

    var text = fmt(elapsed);
    el.textContent = text;
    // Keep the Ist/Δ figures in sync with the ticking main display.
    var ist = document.getElementById('timer-ist');
    if (ist) ist.textContent = text;
    var delta = elapsed - Number(el.dataset.planned || 0) * 60;
    var deltaEl = document.getElementById('timer-delta');
    if (deltaEl) deltaEl.textContent = signed(delta);
    var pctEl = document.getElementById('timer-delta-pct');
    if (pctEl) pctEl.textContent = percent(delta, Number(el.dataset.planned || 0) * 60);
  }

  tick();
  setInterval(tick, 1000);
})();
