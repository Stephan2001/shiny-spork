// popup/popup.js
const $ = id => document.getElementById(id);

async function sendMsg(type, payload = undefined) {
  return new Promise(resolve => {
    chrome.runtime.sendMessage({ type, payload }, (resp) => resolve(resp));
  });
}

async function init() {
  const state = await sendMsg('getState');
  $('toggleActive').checked = !!state.isActive;
  $('baseUrls').value = (state.baseUrls || []).join('\n');
  updateStatusText(state);
  refreshCountAndPreview();
}

function updateStatusText(state) {
  const lines = [];
  lines.push('Active: ' + (state.isActive ? 'Yes' : 'No'));
  if (state.onlyCurrentWindow && state.targetWindowId != null) {
    lines.push('Window restricted: ' + state.targetWindowId);
  }
  if (state.onlyThisTab && state.targetTabId != null) {
    lines.push('Tab restricted: ' + state.targetTabId);
  }
  $('statusText').innerText = lines.join(' · ');
}

$('toggleActive').addEventListener('change', async (e) => {
  await sendMsg('setActive', e.target.checked);
  const s = await sendMsg('getState');
  updateStatusText(s);
});

$('saveBase').addEventListener('click', async () => {
  const raw = $('baseUrls').value.split('\n').map(x => x.trim()).filter(x => x);
  await sendMsg('setBaseUrls', raw);
  const s = await sendMsg('getState');
  updateStatusText(s);
  alert('Saved base URLs.');
});

$('useWindow').addEventListener('click', async () => {
  const r = await sendMsg('useCurrentWindow');
  if (r && r.ok) {
    const s = await sendMsg('getState');
    updateStatusText(s);
    alert('Now restricted to window ' + r.targetWindowId);
  } else {
    alert('Could not set current window.');
  }
});

$('useTab').addEventListener('click', async () => {
  const r = await sendMsg('useCurrentTab');
  if (r && r.ok) {
    const s = await sendMsg('getState');
    updateStatusText(s);
    alert('Now restricted to tab ' + r.targetTabId);
  } else {
    alert('Could not set current tab.');
  }
});

$('clearTargets').addEventListener('click', async () => {
  await sendMsg('clearTargets');
  const s = await sendMsg('getState');
  updateStatusText(s);
  alert('Cleared window/tab restrictions.');
});

$('clearEntries').addEventListener('click', async () => {
  if (!confirm('Clear all recorded entries?')) return;
  await sendMsg('clearEntries');
  refreshCountAndPreview();
});

$('exportCsv').addEventListener('click', async () => {
  const resp = await sendMsg('getEntries');
  const entries = resp.entries || [];
  if (entries.length === 0) {
    alert('No entries to export.');
    return;
  }
  const csv = entriesToCsv(entries);
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `url-records-${Date.now()}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
});

function entriesToCsv(entries) {
  const esc = (s) => {
    if (s == null) return '';
    const str = String(s);
    return `"${str.replace(/"/g, '""')}"`;
  };
  const rows = entries.map(e => [esc(e.url)].join(',')).join('\n');
  return rows;
}

async function refreshCountAndPreview() {
  const resp = await sendMsg('getEntries');
  const entries = resp.entries || [];
  $('count').innerText = entries.length;
  const preview = entries.slice(-10).reverse().map(e => `${e.timestamp} — ${e.url}`).join('\n');
  $('preview').innerText = preview || '(no entries yet)';
}

init();

// refresh preview when popup is focused again
window.addEventListener('focus', () => refreshCountAndPreview());
