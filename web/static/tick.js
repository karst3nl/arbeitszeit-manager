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

  function aw(seconds, minutesPerAW) {
    // The admin can change the factor; the server sends it with the fragment.
    var factor = Number(minutesPerAW) || 6;
    var magnitude = Math.round(Math.abs(seconds) / 60 / factor * 10) / 10;
    var value = seconds < 0 ? -magnitude : magnitude;
    var text = value.toFixed(1).replace('.', ',').replace(/,0$/, '');
    return text + ' AW';
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
    var factor = el.dataset.awMinutes;
    var istAw = document.getElementById('timer-ist-aw');
    if (istAw) istAw.textContent = aw(elapsed, factor);
    var deltaAw = document.getElementById('timer-delta-aw');
    if (deltaAw) deltaAw.textContent = aw(delta, factor);
  }

  tick();
  setInterval(tick, 1000);

  // The dashboard listens for order changes from other users and reloads its
  // order view immediately. The 30-second HTMX poll remains as a fallback.
  var refreshTimer = null;
  function refreshOrders() {
    if (refreshTimer) clearTimeout(refreshTimer);
    refreshTimer = setTimeout(function () {
      refreshTimer = null;
      var view = document.getElementById('orders-view');
      if (!view || typeof htmx === 'undefined') return;
      htmx.ajax('GET', view.getAttribute('hx-get') || '/orders', {
        target: '#orders-view',
        swap: 'innerHTML'
      });
    }, 120);
  }

  function watchOrders() {
    if (!document.getElementById('orders-view') || typeof EventSource === 'undefined') return;
    var stream = new EventSource('/events');
    stream.addEventListener('orders', refreshOrders);
    // EventSource reconnects automatically after a network interruption.
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', watchOrders);
  } else {
    watchOrders();
  }
})();
