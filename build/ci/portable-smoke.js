/* Native platform acceptance: actual production renderer, DOM and local PTYs.
 * The isolated fixture supplies files; no application operations are mocked. */
(async () => {
  const p = window.__dragfmSmokePlan, checks = [], cwd = {}, terminalData = {};
  let stage = 'startup';
  const pause = (ms) => new Promise((r) => setTimeout(r, ms));
  const assert = (ok, message) => { if (!ok) throw new Error(message); };
  const wait = async (description, test, timeout = 20000) => {
    stage = description;
    const end = Date.now() + timeout;
    while (Date.now() < end) { const value = await test(); if (value) return value; await pause(40); }
    throw new Error('Timeout: ' + description);
  };
  const same = (a, b) => p.platform === 'windows' ? a?.replaceAll('\\', '/').toLowerCase() === b?.replaceAll('\\', '/').toLowerCase() : a === b;
  const pane = (id) => document.querySelector(`.file-pane[data-pane="${id}"]`);
  const row = (id, name) => [...pane(id).querySelectorAll('.file-row')].find((r) => r.querySelector('.file-name')?.title === name);
  const set = (element, value) => {
    assert(element, 'input missing');
    const prototype = element instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value);
    element.dispatchEvent(new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  };
  const key = (element, value, code) => element.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key: value, code: value, keyCode: code, which: code }));
  const button = (text) => [...document.querySelectorAll('button')].find((e) => e.textContent.trim() === text);
  const hit = (element) => {
    const r = element.getBoundingClientRect();
    assert(r.width > 20 && r.height >= 20, 'file row has no usable geometry');
    const x = r.left + Math.min(100, r.width / 2), y = r.top + r.height / 2;
    const target = document.elementFromPoint(x, y);
    assert(target && element.contains(target), 'file row is obscured or overlaps another row');
    return { target, x, y };
  };
  const layout = () => {
    for (const id of ['left', 'right']) {
      const part = pane(id), viewport = part.querySelector('.file-viewport'), vr = viewport.getBoundingClientRect();
      const header = part.querySelector('.file-table-header').getBoundingClientRect();
      const terminal = part.querySelector('.terminal-panel').getBoundingClientRect();
      assert(vr.height >= 30 && header.bottom <= vr.top + 1 && vr.bottom <= terminal.top + 1, 'file/terminal layout overlaps');
      const rows = [...part.querySelectorAll('.file-row')].filter((r) => { const b = r.getBoundingClientRect(); return b.top >= vr.top && b.bottom <= vr.bottom; });
      for (let i = 0; i < rows.length; i++) {
        hit(rows[i]);
        if (i) assert(rows[i].getBoundingClientRect().top >= rows[i-1].getBoundingClientRect().bottom - 1, 'file rows overlap');
      }
      for (const selector of ['.file-size', '.file-modified', '.file-mode']) {
        if (rows[0]) assert(getComputedStyle(rows[0].querySelector(selector)).display !== 'none', 'metadata column hidden');
      }
    }
    const tasks = document.querySelector('.task-pane').getBoundingClientRect();
    assert(tasks.right <= window.innerWidth + 1 && tasks.bottom <= window.innerHeight + 1, 'task pane overflows');
  };
  const navigate = async (id, path) => {
    const input = pane(id).querySelector('.path-form input'); set(input, path); await pause(50);
    pane(id).querySelector('.path-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await wait(id + ' file navigation', () => same(input.value, path) && !pane(id).querySelector('.loading-line'));
    await wait(id + ' PTY navigation', () => same(cwd[id]?.path, path));
  };
  const finish = (id, state = 'succeeded') => wait('job ' + state, async () => {
    const job = (await window.go.webgui.App.JobSnapshot()).find((j) => j.id === id);
    if (job && ['failed', 'cancelled', 'succeeded'].includes(job.state)) { assert(job.state === state, job.description + ': ' + job.message); return job; }
    return false;
  }, 30000);
  try {
    await wait('Wails bridge', () => window.go?.webgui?.App && window.runtime?.EventsOn);
    const api = window.go.webgui.App;
    window.runtime.EventsOn('terminal:cwd', (event) => { cwd[event.pane] = event; });
    window.runtime.EventsOn('terminal:data', (event) => { terminalData[event.pane] = ((terminalData[event.pane] || '') + atob(event.data)).slice(-65536); });
    const status = await api.VaultStatus();
    assert(status.exists === (p.phase === 'restore') && !status.unlocked, 'wrong isolated vault startup state');
    await wait('unlock form', () => document.querySelector('.unlock-panel form'));
    const passwords = document.querySelectorAll('.unlock-panel input[type=password]');
    set(passwords[0], p.password);
    if (p.phase === 'exercise') { set(passwords[1], p.password); set(document.querySelector('.unlock-panel input:not([type=password])'), 'Disposable platform fixture'); }
    await pause(50);
    document.querySelector('.unlock-panel form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await wait('workspace and PTYs', () => document.querySelector('.workspace-grid') && cwd.left && cwd.right, 45000);
    checks.push('native vault form and two real local PTYs');
    if (p.phase === 'restore') {
      const history = (await api.Bootstrap()).history;
      assert(p.historyIDs.every((id) => history.some((j) => j.id === id)), 'history missing in new process');
      assert((await api.JobSnapshot()).length === 0, 'pending work restored');
      checks.push('native process restart restores history but not pending work');
    } else {
      await navigate('left', p.source); await navigate('right', p.target);
      layout(); checks.push('visible hit-tested rows and metadata columns; file/PTY geometry');
      const transfer = async (name, choice) => {
        const source = hit(await wait('source row', () => row('left', name)));
        const destination = hit(await wait('destination row', () => row('right', 'archive')));
        const init = { bubbles: true, cancelable: true, button: 0, buttons: 1, pointerId: 1, pointerType: 'mouse', clientX: source.x, clientY: source.y };
        source.target.dispatchEvent(new PointerEvent('pointerdown', init));
        window.dispatchEvent(new PointerEvent('pointermove', { ...init, clientX: destination.x, clientY: destination.y }));
        await wait('visible drag highlight', () => row('right', 'archive').classList.contains('drop-target'));
        window.dispatchEvent(new PointerEvent('pointerup', { ...init, buttons: 0, clientX: destination.x, clientY: destination.y }));
        const action = await wait('transfer confirmation', () => [...document.querySelectorAll('.choice-button')].find((e) => e.querySelector('strong')?.textContent === choice));
        const previous = new Set((await api.JobSnapshot()).map((j) => j.id));
        action.click();
        const job = await wait('transfer admitted', async () => (await api.JobSnapshot()).find((j) => !previous.has(j.id)));
        await finish(job.id);
        await wait('transfer UI refreshed', () => !document.querySelector('.drop-confirm') && !pane('left').querySelector('.loading-line'));
      };
      await transfer('source.txt', '复制');
      await transfer('source.txt', '移动并覆盖');
      await wait('moved source removed', () => !row('left', 'source.txt'));
      checks.push('coordinate-hit-tested native copy and overwrite move');
      const hash = await wait('hash row', () => row('left', 'hash.txt')); hit(hash).target.click();
      await wait('hash selection', () => hash.classList.contains('selected')); key(hash, 'h', 72);
      await wait('actual SHA256', async () => (await api.JobSnapshot()).some((j) => j.state === 'succeeded' && j.message.includes(p.hash)));
      const remove = await wait('delete row', () => row('left', 'delete.txt')); hit(remove).target.click();
      await wait('delete selection', () => remove.classList.contains('selected')); key(remove, 'd', 68);
      await wait('delete dialog', () => document.querySelector('.delete-confirm')); document.querySelector('.danger-button').click();
      await wait('actual deletion', () => !row('left', 'delete.txt'));
      checks.push('native hash and confirmed deletion');
      await navigate('left', p.spacePath);
      const oldSequence = cwd.left.sequence;
      const input = pane('left').querySelector('.xterm-helper-textarea'); input.focus();
      // Paste through xterm's own handler, then send Enter. PowerShell and POSIX
      // both accept this literal echo; the distinctive marker is not a mock.
      const data = new DataTransfer(); data.setData('text/plain', 'echo DRAGFM_NATIVE_PLATFORM_OK');
      input.dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data }));
      await pause(100); key(input, 'Enter', 13);
      await wait('native terminal command', () => terminalData.left.includes('DRAGFM_NATIVE_PLATFORM_OK') && cwd.left.sequence > oldSequence);
      await navigate('left', p.source);
      checks.push('quoted/space directory navigation and native xterm command/prompt');
      set(document.querySelector('.command-bar select'), '控制机');
      set(document.querySelector('.command-input input'), p.platform === 'windows' ? 'ping -n 31 127.0.0.1 >nul' : 'sleep 30');
      await pause(50); const before = new Set((await api.JobSnapshot()).map((j) => j.id));
      document.querySelector('.command-bar').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
      const command = await wait('local command running', async () => (await api.JobSnapshot()).find((j) => !before.has(j.id) && j.state === 'running'));
      const stop = await wait('running cancel control', () => document.querySelector('.running-line button')); const started = Date.now(); stop.click();
      await finish(command.id, 'cancelled'); assert(Date.now() - started < 5000, 'local cancellation exceeded five seconds');
      checks.push('bounded native command cancellation including child processes');
      window.runtime.WindowSetSize(1080, 680);
      await wait('minimum window', () => window.innerWidth <= 1100); await pause(100); layout();
      checks.push('minimum-window native geometry and hit testing');
    }
    const history = (await api.Bootstrap()).history;
    window.runtime.EventsEmit('__dragfm_native_smoke_result__', JSON.stringify({ success: true, phase: p.phase, checks, historyIDs: history.map((j) => j.id) }));
  } catch (error) {
    window.runtime?.EventsEmit('__dragfm_native_smoke_result__', JSON.stringify({ success: false, phase: p.phase, stage, checks, error: String(error).replaceAll(p.password, '[redacted]') }));
  }
})();
