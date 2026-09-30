(function () {
  "use strict";

  var API = (window.API_BASE_URL || "http://localhost:3000").replace(/\/$/, "");
  var STORAGE_KEY = "goproxyx.session";

  // ---------- tiny DOM helpers ----------
  var $ = function (id) { return document.getElementById(id); };
  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    attrs = attrs || {};
    Object.keys(attrs).forEach(function (k) {
      if (k === "text") node.textContent = attrs[k];
      else if (k === "html") node.innerHTML = attrs[k];
      else node.setAttribute(k, attrs[k]);
    });
    (children || []).forEach(function (c) { node.appendChild(c); });
    return node;
  }

  // ---------- notices (replaces alert() popups) ----------
  var noticeTimer = null;
  function notify(text, tone) {
    var n = $("notice");
    n.textContent = text;
    n.setAttribute("data-tone", tone || "success");
    n.hidden = false;
    clearTimeout(noticeTimer);
    noticeTimer = setTimeout(function () { n.hidden = true; }, 8000);
  }

  // ---------- session ----------
  function getSession() {
    try { return JSON.parse(localStorage.getItem(STORAGE_KEY) || "null"); }
    catch (e) { return null; }
  }
  function setSession(session) { localStorage.setItem(STORAGE_KEY, JSON.stringify(session)); }
  function clearSession() { localStorage.removeItem(STORAGE_KEY); }

  // ---------- API calls ----------
  function api(path, options) {
    options = options || {};
    var headers = Object.assign({}, options.headers || {});
    var session = getSession();
    if (session && session.token) headers["Authorization"] = "Bearer " + session.token;
    if (options.body) headers["Content-Type"] = "application/json";

    return fetch(API + path, {
      method: options.method || "GET",
      headers: headers,
      body: options.body ? JSON.stringify(options.body) : undefined,
    }).then(function (res) {
      updateGauge(res.headers);
      if (res.status === 204) return null;
      return res.json().catch(function () { return null; }).then(function (data) {
        if (!res.ok) {
          var err = new Error((data && data.error && data.error.message) || res.statusText);
          err.status = res.status;
          err.code = data && data.error && data.error.code;
          err.retryAfter = parseInt(res.headers.get("Retry-After"), 10) || 0;
          throw err;
        }
        return data;
      });
    });
  }

  // ---------- rate-limit gauge ----------
  function updateGauge(headers) {
    var limit = headers.get("X-RateLimit-Limit");
    var remaining = headers.get("X-RateLimit-Remaining");
    if (limit == null || remaining == null) return;
    limit = parseInt(limit, 10);
    remaining = Math.max(0, parseInt(remaining, 10));

    var gauge = $("gauge");
    gauge.hidden = false;
    $("gaugeValue").textContent = remaining + " / " + limit;
    var pct = limit > 0 ? Math.round((remaining / limit) * 100) : 0;
    $("gaugeFill").style.width = pct + "%";
    gauge.classList.toggle("gauge--low", remaining <= Math.ceil(limit * 0.3));
  }

  // ---------- login gate ----------
  function renderLoginGate() {
    api("/auth/demo-users").then(function (users) {
      var container = $("demoUsers");
      container.innerHTML = "";
      users.forEach(function (u) {
        var btn = el("button", { class: "seller-btn", type: "button" }, [
          el("span", { class: "seller-btn__name", text: u.name }),
          el("span", { class: "seller-btn__id", text: "user id " + u.id }),
        ]);
        btn.addEventListener("click", function () { login(String(u.id), u.name); });
        container.appendChild(btn);
      });
    }).catch(function (err) {
      $("demoUsers").innerHTML = "";
      $("demoUsers").appendChild(el("p", {
        class: "muted",
        text: "Couldn't reach the gateway (" + err.message + "). Is it running, and is API_BASE_URL set correctly?",
      }));
    });
  }

  function login(userId, name) {
    api("/auth/login", { method: "POST", body: { user_id: userId } }).then(function (res) {
      setSession({ token: res.token, userId: res.user.id, name: res.user.name || name });
      showApp();
    }).catch(function (err) {
      $("demoUsers").appendChild(el("p", { class: "muted", text: "Couldn't log in: " + err.message }));
    });
  }

  function logout() {
    clearSession();
    location.reload();
  }

  // ---------- item grid ----------
  var currentQuery = "";
  var nextCursor = null;
  var retryTimer = null;

  function loadItems(opts) {
    opts = opts || {};
    var append = !!opts.append;
    var status = $("itemsStatus");
    status.textContent = append ? "Loading more\u2026" : "Loading items\u2026";
    status.removeAttribute("data-tone");

    var params = [];
    if (currentQuery) params.push("q=" + encodeURIComponent(currentQuery));
    if (append && nextCursor) params.push("after=" + encodeURIComponent(nextCursor));
    var qs = params.length ? "?" + params.join("&") : "";

    api("/items" + qs).then(function (res) {
      status.textContent = "";
      // Older responses (or a mismatched backend) could still be a bare
      // array; handle both shapes rather than throwing on res.items.
      var items = Array.isArray(res) ? res : (res && res.items) || [];
      nextCursor = Array.isArray(res) ? null : (res && res.next_cursor) || null;
      renderItems(items, { append: append });
    }).catch(function (err) {
      if (err.status === 401) { clearSession(); showLoginGate(); return; }
      if (err.status === 429) {
        // The gateway told us exactly how long to wait (Retry-After); honour
        // it and try again automatically instead of leaving a dead end.
        var wait = Math.min(Math.max(err.retryAfter, 1), 10);
        status.textContent = "The gateway's rate limiter is holding requests back \u2014 retrying in " + wait + "s\u2026";
        status.removeAttribute("data-tone");
        clearTimeout(retryTimer);
        retryTimer = setTimeout(function () { loadItems(opts); }, wait * 1000);
        return;
      }
      status.textContent = "Couldn't load items: " + err.message;
      status.setAttribute("data-tone", "error");
    });
  }

  function renderItems(items, opts) {
    opts = opts || {};
    var grid = $("itemGrid");
    if (!opts.append) grid.innerHTML = "";
    var session = getSession();
    var loadMoreBtn = $("loadMoreBtn");
    if (loadMoreBtn) loadMoreBtn.remove();

    if (items.length === 0 && !opts.append) {
      grid.appendChild(el("div", { class: "empty-state" }, [
        el("strong", { text: currentQuery ? "No items match \u201c" + currentQuery + "\u201d" : "The table is empty" }),
        el("span", { text: currentQuery ? "Try a different search, or clear it to see everything on sale." : "List the first item above to get things started." }),
      ]));
      return;
    }

    items.forEach(function (item) {
      var isOwn = session && Number(session.userId) === Number(item.seller_id);
      var isSold = item.status !== "on_sale";

      var priceStr = "\u00a5" + Number(item.price_jpy).toLocaleString("ja-JP");
      var card = el("article", { class: "tag-card" }, [
        el("h3", { class: "tag-card__title", text: item.title }),
        el("p", { class: "tag-card__desc", text: item.description || "No description provided." }),
        el("div", { class: "tag-card__meta" }, [
          el("span", { class: "tag-card__price", text: priceStr }),
          el("span", {
            class: "status-pill", "data-status": item.status,
            text: isSold ? "sold" : "on sale",
          }),
        ]),
      ]);

      if (!isSold) {
        var buyBtn = el("button", {
          class: "btn btn--buy", type: "button",
        }, [document.createTextNode(isOwn ? "Your listing" : "Buy")]);
        if (isOwn) buyBtn.disabled = true;
        buyBtn.addEventListener("click", function () { purchase(item, buyBtn); });
        card.appendChild(buyBtn);
      }

      grid.appendChild(card);
    });

    if (nextCursor) {
      var loadMore = el("button", {
        id: "loadMoreBtn", class: "btn btn--ghost load-more", type: "button",
      }, [document.createTextNode("Load more")]);
      loadMore.addEventListener("click", function () {
        loadMore.disabled = true;
        loadMore.textContent = "Loading\u2026";
        loadItems({ append: true });
      });
      grid.parentNode.insertBefore(loadMore, grid.nextSibling);
    }
  }

  function purchase(item, buttonEl) {
    buttonEl.disabled = true;
    buttonEl.textContent = "Buying\u2026";
    api("/items/" + item.id + "/purchase", { method: "POST" }).then(function (order) {
      var price = "\u00a5" + Number(order.price_jpy).toLocaleString("ja-JP");
      notify("Purchased \u201c" + item.title + "\u201d for " + price + ". It's now marked sold and has left the table.", "success");
      loadItems();
    }).catch(function (err) {
      if (err.code === "already_sold") notify("Too slow \u2014 someone else just bought \u201c" + item.title + "\u201d.", "error");
      else if (err.code === "own_item") notify("You can't buy your own listing.", "error");
      else if (err.status === 429) notify("You've hit the gateway's rate limit. Wait a moment and try again.", "error");
      else notify("Purchase failed: " + err.message, "error");
      loadItems();
    });
  }

  // ---------- new item form ----------
  function initNewItemForm() {
    var toggle = $("newItemToggle");
    var form = $("newItemForm");
    toggle.addEventListener("click", function () {
      var open = !form.hidden;
      form.hidden = open;
      toggle.setAttribute("aria-expanded", String(!open));
      if (!open) $("title").focus();
    });

    form.addEventListener("submit", function (e) {
      e.preventDefault();
      var status = $("newItemStatus");
      var submitBtn = form.querySelector("button[type=submit]");
      var payload = {
        title: $("title").value.trim(),
        description: $("description").value.trim(),
        price_jpy: parseInt($("price").value, 10),
      };
      if (!payload.title || !payload.price_jpy || payload.price_jpy <= 0) {
        status.textContent = "A title and a price above \u00a50 are required.";
        status.setAttribute("data-tone", "error");
        return;
      }
      submitBtn.disabled = true;
      status.removeAttribute("data-tone");
      status.textContent = "Listing\u2026";

      api("/items", { method: "POST", body: payload }).then(function () {
        status.textContent = "Listed.";
        status.setAttribute("data-tone", "success");
        form.reset();
        form.hidden = true;
        toggle.setAttribute("aria-expanded", "false");
        loadItems();
      }).catch(function (err) {
        status.textContent = err.status === 429
          ? "Rate limited \u2014 wait a moment and try again."
          : "Couldn't list the item: " + err.message;
        status.setAttribute("data-tone", "error");
      }).finally(function () { submitBtn.disabled = false; });
    });
  }

  // ---------- search ----------
  function initSearch() {
    var input = $("search");
    var timer = null;
    input.addEventListener("input", function () {
      clearTimeout(timer);
      timer = setTimeout(function () {
        currentQuery = input.value.trim();
        loadItems();
      }, 300);
    });
  }

  // ---------- view switching ----------
  function showLoginGate() {
    $("loginGate").hidden = false;
    $("app").hidden = true;
    $("whoami").hidden = true;
    renderLoginGate();
  }

  function showApp() {
    var session = getSession();
    $("loginGate").hidden = true;
    $("app").hidden = false;
    $("whoami").hidden = false;
    $("whoamiName").textContent = session.name || ("User " + session.userId);
    loadItems();
  }

  // ---------- boot ----------
  document.addEventListener("DOMContentLoaded", function () {
    $("logoutBtn").addEventListener("click", logout);
    initNewItemForm();
    initSearch();

    var session = getSession();
    if (session && session.token) showApp();
    else showLoginGate();
  });
})();
