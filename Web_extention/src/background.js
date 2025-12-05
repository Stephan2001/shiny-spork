let isActive = false;
let baseUrls = [];
let onlyCurrentWindow = false;
let targetWindowId = null;
let onlyThisTab = false;
let targetTabId = null;

const lastRecordedUrlByTab = new Map();

const STORAGE_KEYS = {
  entries: 'rec_entries',
  settings: 'rec_settings'
};

async function loadSettings() {
  const data = await chrome.storage.local.get(STORAGE_KEYS.settings);
  const s = data[STORAGE_KEYS.settings] || {};
  isActive = !!s.isActive;
  baseUrls = s.baseUrls || [];
  onlyCurrentWindow = !!s.onlyCurrentWindow;
  targetWindowId = s.targetWindowId ?? null;
  onlyThisTab = !!s.onlyThisTab;
  targetTabId = s.targetTabId ?? null;
}
loadSettings();

function saveSettings() {
  chrome.storage.local.set({
    [STORAGE_KEYS.settings]: {
      isActive, baseUrls, onlyCurrentWindow, targetWindowId, onlyThisTab, targetTabId
    }
  });
}

function matchesBase(url) {
  if (!url) return false;
  if (baseUrls.length === 0) return true;
  return baseUrls.some(base => url.startsWith(base));
}

async function pushEntry(entry) {
  const d = await chrome.storage.local.get(STORAGE_KEYS.entries);
  const arr = d[STORAGE_KEYS.entries] || [];
  arr.push(entry);
  await chrome.storage.local.set({ [STORAGE_KEYS.entries]: arr });
}

// recordIfMatch now accepts a tab object and records irrespective of tab.active
async function recordIfMatch(tab, reason = '') {
  try {
    if (!tab || !tab.url) return;
    if (!isActive) return;

    // window restriction
    if (onlyCurrentWindow && typeof targetWindowId === 'number' && tab.windowId !== targetWindowId) return;

    // tab restriction
    if (onlyThisTab && typeof targetTabId === 'number' && tab.id !== targetTabId) return;

    if (!matchesBase(tab.url)) return;

    // deduplicate per-tab: skip if same URL was already recorded for this tab recently
    const last = lastRecordedUrlByTab.get(tab.id);
    if (last === tab.url) {
      // optional: you can still record if reason==='activated' or on a time threshold
      return;
    }
    lastRecordedUrlByTab.set(tab.id, tab.url);

    const entry = {
      timestamp: new Date().toISOString(),
      url: tab.url,
      title: tab.title || '',
      tabId: tab.id,
      windowId: tab.windowId,
      reason
    };

    await pushEntry(entry);
    // for debugging uncomment: console.log('Recorded', entry);
  } catch (err) {
    console.error('recordIfMatch error', err);
  }
}

// tabs.onActivated => user switched to a tab (active in focused window)
chrome.tabs.onActivated.addListener(async (activeInfo) => {
  try {
    const tab = await chrome.tabs.get(activeInfo.tabId);
    await recordIfMatch(tab, 'activated');
  } catch (err) { /* ignore */ }
});

// tabs.onUpdated => page updated (navigations, loads, including background tabs)
// changeInfo.url is present for navigations; status==='complete' signals load.
chrome.tabs.onUpdated.addListener(async (tabId, changeInfo, tab) => {
  try {
    // we only need to run when URL changed or finished loading; but it's okay to call recordIfMatch
    if (changeInfo.url || changeInfo.status === 'complete') {
      await recordIfMatch(tab, changeInfo.url ? 'url-change' : 'complete');
    }
  } catch (err) {}
});

// message interface (same as before)
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  (async () => {
    const { type, payload } = message;
    if (type === 'getState') {
      sendResponse({ isActive, baseUrls, onlyCurrentWindow, targetWindowId, onlyThisTab, targetTabId });
      return;
    }
    if (type === 'setActive') {
      isActive = !!payload;
      saveSettings();
      sendResponse({ ok: true });
      return;
    }
    if (type === 'setBaseUrls') {
      baseUrls = Array.isArray(payload) ? payload : [];
      saveSettings();
      sendResponse({ ok: true });
      return;
    }
    if (type === 'useCurrentWindow') {
      try {
        const win = await chrome.windows.getCurrent();
        targetWindowId = win.id;
        onlyCurrentWindow = true;
        saveSettings();
        sendResponse({ ok: true, targetWindowId });
      } catch (e) { sendResponse({ ok: false, error: e.message }); }
      return;
    }
    if (type === 'useCurrentTab') {
      try {
        const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
        if (tabs && tabs[0]) {
          targetTabId = tabs[0].id;
          onlyThisTab = true;
          saveSettings();
          sendResponse({ ok: true, targetTabId });
        } else {
          sendResponse({ ok: false, error: 'No active tab' });
        }
      } catch (e) { sendResponse({ ok: false, error: e.message }); }
      return;
    }
    if (type === 'clearTargets') {
      onlyCurrentWindow = false;
      targetWindowId = null;
      onlyThisTab = false;
      targetTabId = null;
      saveSettings();
      sendResponse({ ok: true });
      return;
    }
    if (type === 'getEntries') {
      const d = await chrome.storage.local.get(STORAGE_KEYS.entries);
      sendResponse({ entries: d[STORAGE_KEYS.entries] || [] });
      return;
    }
    if (type === 'clearEntries') {
      await chrome.storage.local.set({ [STORAGE_KEYS.entries]: [] });
      // clear in-memory dedupe map too
      lastRecordedUrlByTab.clear();
      sendResponse({ ok: true });
      return;
    }

    // SPA navigation message from content script
    if (type === 'spaUrlChange') {
      // payload should be { tabId, url, title }
      const tab = {
        id: payload.tabId,
        url: payload.url,
        title: payload.title || '',
        windowId: payload.windowId
      };
      await recordIfMatch(tab, 'spa');
      sendResponse({ ok: true });
      return;
    }

    sendResponse({ ok: false, error: 'unknown message' });
  })();
  return true;
});
