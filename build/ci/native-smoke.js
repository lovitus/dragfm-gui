/* Executed only by --native-smoke-dir in the production Wails webview.
 * Uses the real DOM, Wails bridge, persistent PTYs and SSH backend. No mocks.
 * window.__dragfmSmokePlan is supplied by the disposable hosted-runner fixture.
 */
(async () => {
  const plan = window.__dragfmSmokePlan;
  const checks = [];
  let stage = 'native startup';
  const cwd = {};
  const terminalOutput = {};
  const pendingCancellation = {};
  const fileBrowsing = {};
  const events = [];
  const wakeups = new Set();
  const wake = () => { for (const notify of wakeups) notify(); };
  const trusted = { pointerdown: 0, pointerup: 0, click: 0, dblclick: 0, wheel: 0, keydown: 0, keypress: 0, keyup: 0,
    beforeinput: 0, input: 0, compositionstart: 0, compositionupdate: 0, compositionend: 0 };
  let lastPointer, lastRelease, lastClick, lastDoubleClick, lastWheel;
  for (const kind of Object.keys(trusted)) document.addEventListener(kind, (event) => {
    if (event.isTrusted) {
      trusted[kind]++;
      if (kind === 'pointerdown') lastPointer = { x: event.clientX, y: event.clientY, path: event.composedPath() };
      if (kind === 'pointerup') lastRelease = event.composedPath();
      if (kind === 'click') lastClick = event.composedPath();
      if (kind === 'dblclick') lastDoubleClick = event.composedPath();
      if (kind === 'wheel') lastWheel = event.composedPath();
    }
    wake();
  }, true);
  window.addEventListener('resize', wake);
  document.addEventListener('scroll', wake, true);
  const inputReplies = new Map();
  let inputSequence = 0;
  let challengeFailure = '';
  let declineLocalSudo = false;
  const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async (description, predicate, timeout = 20000) => {
    stage = description;
    window.runtime?.LogInfo('NATIVE_SMOKE ' + stage);
    // Observe renderer/runtime events instead of repeatedly polling RPCs.
    return await new Promise((resolve, reject) => {
      let settled = false, checking = false, changed = false;
      const finish = (value, error) => {
        if (settled) return;
        settled = true; clearTimeout(timer); observer.disconnect(); wakeups.delete(notify);
        if (error) reject(error); else resolve(value);
      };
      const check = async () => {
        if (settled || checking) return;
        checking = true; changed = false;
        try {
          if (challengeFailure) throw new Error(challengeFailure);
          const result = await predicate();
          if (result) finish(result);
        } catch (error) { finish(undefined, error); }
        finally { checking = false; if (changed && !settled) queueMicrotask(check); }
      };
      const notify = () => { changed = true; void check(); };
      const observer = new MutationObserver(notify);
      observer.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
      const timer = setTimeout(() => {
        const errors = [...document.querySelectorAll('.pane-error,.operation-error,.form-error,.save-status.error')].map((item) => item.textContent).join('; ');
        finish(undefined, new Error(`Timeout: ${description}${errors ? `; ${errors}` : ''}`));
      }, timeout);
      wakeups.add(notify); notify();
    });
  };
  const native = (action, detail = {}) => new Promise((resolve, reject) => {
    const id = ++inputSequence;
    const timer = setTimeout(() => { inputReplies.delete(id); reject(new Error(`OS input ${action} did not acknowledge`)); }, 15000);
    inputReplies.set(id, (result) => { clearTimeout(timer); if (result.error) reject(new Error(result.error)); else resolve(); });
    window.runtime.EventsEmit('__dragfm_native_input__', JSON.stringify({ id, action, ...detail, viewport: { width: innerWidth, height: innerHeight } }));
  });
  const point = (element, blank = false) => {
    assert(element, 'missing native input target');
    const rect = element.getBoundingClientRect();
    assert(rect.width > 0 && rect.height > 0 && rect.left >= 0 && rect.top >= 0 && rect.right <= innerWidth + 1 && rect.bottom <= innerHeight + 1, 'native control is clipped or outside viewport');
    return { x: rect.x + rect.width / 2, y: blank ? rect.bottom - 12 : rect.y + rect.height / 2 };
  };
  const nativeClick = async (element, blank = false) => {
    const location = point(element, blank), before = trusted.pointerdown, releases = trusted.pointerup, clicks = trusted.click;
    // Window bounds alone do not account for an overflow-clipped queue row.
    // Refuse a hit on a different control; never count that as a user click.
    assert(element.contains(document.elementFromPoint(location.x, location.y)), 'native click target is clipped or covered');
    if (blank) assert(!document.elementFromPoint(location.x, location.y)?.closest('.file-row'), 'requested empty-space click actually hits a file row');
    await native('click', { point: location });
    await wait('trusted native click', () => trusted.pointerdown > before);
    assert(Math.abs(lastPointer.x - location.x) <= 2 && Math.abs(lastPointer.y - location.y) <= 2, 'native screen-to-webview pointer mapping is wrong');
    assert(lastPointer.path.includes(element), 'OS pointerdown landed on a different control');
    await wait('trusted release on intended control', () => trusted.pointerup > releases);
    assert(lastRelease.includes(element), 'OS pointerup landed on a different control');
    await wait('trusted click on intended control', () => trusted.click > clicks);
    // Retain the dispatch-time path: a successful dialog click can unmount
    // its button before the native acknowledgement reaches this renderer.
    assert(lastClick.includes(element), 'OS click landed on a different control');
  };
  const nativeKeys = async (...keys) => {
    const before = trusted.keydown;
    await native('keys', { keys });
    await wait('trusted native key', () => trusted.keydown > before);
  };
  const nativeText = async (text) => {
    // Keep each acknowledged OS action within its existing 15s deadline at
    // the documented macOS catch-up cadence, even for the longer shell input.
    for (let start = 0; start < text.length; start += 32) {
      await native('text', { text: text.slice(start, start + 32) });
    }
  };
  const input = (element, value) => {
    assert(element, 'missing input');
    const prototype = element instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value);
    element.dispatchEvent(new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  };
  const button = (text, parent = document) => [...parent.querySelectorAll('button')].find((item) => item.textContent.trim() === text);
  const click = async (text, parent = document) => nativeClick(await wait(`button ${text}`, () => button(text, parent)));
  const pane = (which) => document.querySelector(`.file-pane[data-pane="${which}"]`);
  const rows = (which) => [...pane(which).querySelectorAll('.file-row')];
  const row = (which, name) => rows(which).find((item) => item.querySelector('.file-name').getAttribute('title') === name);
  const pathInput = (which) => pane(which).querySelector('.path-form input');
  const navigate = async (which, path) => {
    await wait(`${which} PTY ready`, () => cwd[which]);
    input(pathInput(which), path);
    await pause(40);
    pane(which).querySelector('.path-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await wait(`${which} navigation`, () => pathInput(which).value === path && !pane(which).querySelector('.loading-line'));
    await wait(`${which} file-to-shell cwd`, () => cwd[which]?.path === path);
  };
  const browseMany = async (which, returnPath) => {
    await navigate(which, plan.browse);
    const viewport = pane(which).querySelector('.file-viewport');
    const visible = (element) => {
      if (!element) return false;
      const area = viewport.getBoundingClientRect(), rect = element.getBoundingClientRect();
      return rect.top >= area.top && rect.bottom <= area.bottom &&
        element.contains(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2));
    };
    assert(pane(which).querySelector('.pane-statusbar').textContent.includes(`${plan.browseCount} 项`), 'large directory has the wrong entry count');
    assert(viewport.scrollTop === 0 && viewport.scrollHeight > viewport.clientHeight * 3, 'large directory is not a fresh multi-screen viewport');
    assert(!row(which, plan.browseFile) && !row(which, plan.browseDirectory), 'deep entries were already rendered before OS scrolling');
    const evidence = fileBrowsing[which] = { initialRenderedRows: rows(which).length, scrollActions: 0, initialScrollTop: viewport.scrollTop };
    const rowHeight = rows(which)[0].getBoundingClientRect().height;
    assert(rowHeight > 0, 'initial file rows have no physical height');
    const reachedBand = () => viewport.scrollTop <= 63 * rowHeight && viewport.scrollTop + viewport.clientHeight >= 65 * rowHeight;
    // Drive the original OS wheel adapter, never scrollTop/scrollIntoView or
    // a synthetic scroll event. Every action must land in this file viewport.
    for (let action = 0; action < 24 && !reachedBand(); action++) {
      const wheels = trusted.wheel, previous = viewport.scrollTop, location = point(viewport);
      assert(viewport.contains(document.elementFromPoint(location.x, location.y)), 'file wheel target is covered');
      const clicks = viewport.scrollTop > 63 * rowHeight ? 1 : -8;
      await native('scroll', { point: location, clicks });
      await wait(`${which} trusted file wheel advances its viewport`, () => trusted.wheel > wheels && viewport.scrollTop !== previous);
      assert(lastWheel?.includes(viewport), 'OS wheel landed outside the intended file viewport');
      evidence.scrollActions++;
      evidence.scrollTop = viewport.scrollTop;
    }
    assert(reachedBand(), 'OS scrolling did not reach the deep file band');
    evidence.reachedDeepBand = true;
    await wait(`${which} deep file band is rendered after the trusted wheel`, () => row(which, plan.browseFile) && row(which, plan.browseDirectory));
    evidence.deepEntriesRendered = true;
    await wait(`${which} scrolled deep entries are hit-testable`, () => visible(row(which, plan.browseFile)) && visible(row(which, plan.browseDirectory)));
    evidence.scrollTop = viewport.scrollTop;
    const file = row(which, plan.browseFile), directory = row(which, plan.browseDirectory);
    const date = new Date(plan.browseFileTime * 1000), pad = (number) => String(number).padStart(2, '0');
    const when = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
    assert(file.querySelector('.file-size').textContent === '8.0 KiB' && file.querySelector('.file-mode').textContent === '-rw-r-----' && file.querySelector('.file-modified').textContent === when, 'deep file name and metadata do not describe the same real entry');
    assert(directory.querySelector('.file-size').textContent === '—' && directory.querySelector('.file-mode').textContent === 'drwxr-x---', 'deep directory metadata is wrong');
    evidence.metadata = { name: plan.browseFile, size: file.querySelector('.file-size').textContent,
      mode: file.querySelector('.file-mode').textContent, modified: file.querySelector('.file-modified').textContent };
    await native('capture', { label: `files-${which}-deep` });
    await nativeClick(file);
    await wait(`${which} OS selects only the deep file`, () => file.classList.contains('selected') && rows(which).filter((item) => item.classList.contains('selected')).length === 1);
    const location = point(directory), doubles = trusted.dblclick;
    await native('double-click', { point: location });
    await wait(`${which} trusted double-click hits the deep directory`, () => trusted.dblclick > doubles);
    assert(lastDoubleClick.includes(directory), 'OS double-click landed on a different directory');
    const child = plan.browse + '/' + plan.browseDirectory;
    await wait(`${which} deep directory opens via OS double-click`, () => pathInput(which).value === child && cwd[which]?.path === child && row(which, 'inside.txt'));
    await nativeClick(pane(which).querySelector('.file-viewport'), true);
    await nativeKeys('backspace');
    await wait(`${which} returns from the deep directory and resets scrolling`, () => pathInput(which).value === plan.browse && cwd[which]?.path === plan.browse && pane(which).querySelector('.file-viewport').scrollTop === 0 && row(which, 'entry-0000.bin'));
    evidence.selectedFile = plan.browseFile;
    evidence.openedDirectory = plan.browseDirectory;
    evidence.returnedWithScrollReset = true;
    await native('capture', { label: `files-${which}-returned` });
    await navigate(which, returnPath);
  };
  const drag = async (name, choice, directory = 'archive', sourcePane = 'left', targetPane = 'right', expectedDirectory = plan.target + '/archive') => {
    const source = await wait(`source row ${name}`, () => row(sourcePane, name));
    const target = await wait(`directory row ${directory}`, () => directory === null ? pane(targetPane).querySelector('.file-viewport') : row(targetPane, directory));
    const from = point(source), to = point(target, directory === null);
    if (directory === null) assert(!document.elementFromPoint(to.x, to.y)?.closest('.file-row'), 'empty drop target hits a row');
    const down = trusted.pointerdown, up = trusted.pointerup;
    await native('down', { point: from });
    await wait('trusted drag start', () => trusted.pointerdown > down);
    await native('drag', { point: to });
    await wait('physical directory hover', () => directory === null ? pane(targetPane).classList.contains('drop-current') : target.classList.contains('drop-target'));
    await native('up', { point: to });
    await wait('trusted drag release', () => trusted.pointerup > up);
    const action = await wait(`transfer choice ${choice}`, () => [...document.querySelectorAll('.choice-button')].find((item) => item.querySelector('strong').textContent === choice));
    assert(document.getSelection().toString() === '', 'drag selected page text');
    assert(document.querySelector('.transfer-path').textContent.includes(expectedDirectory), 'drop did not resolve to the hovered directory');
    const modal = action.closest('.modal');
    await wait('modal initial focus', () => modal.contains(document.activeElement));
    // Exercise wraparound, not just one Tab that might happen to stay inside.
    for (let count = 0; count < 5; count++) {
      await nativeKeys('tab');
      assert(modal.contains(document.activeElement), 'OS Tab escaped the transfer dialog');
    }
    const before = new Set((await window.go.webgui.App.JobSnapshot()).map((job) => job.id));
    await nativeClick(action);
    await wait('transfer dialog closes', () => !document.querySelector('.drop-confirm'));
    return await wait(`queued ${name}`, async () => (await window.go.webgui.App.JobSnapshot()).find((job) => !before.has(job.id)));
  };
  const complete = async (id, wanted = 'succeeded') => await wait(`job ${wanted}`, async () => {
    const job = (await window.go.webgui.App.JobSnapshot()).find((entry) => entry.id === id);
    if (job && ['failed', 'cancelled', 'succeeded'].includes(job.state)) {
      assert(job.state === wanted, `${job.description}: ${job.state}: ${job.message}`);
      return job;
    }
    return false;
  }, 110000);
  const queueCommand = async (command) => {
    const before = new Set((await window.go.webgui.App.JobSnapshot()).map((job) => job.id));
    input(document.querySelector('.command-bar select'), '右栏');
    input(document.querySelector('.command-input input'), command);
    await pause(40);
    document.querySelector('.command-bar').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    return await wait('command admitted', async () => (await window.go.webgui.App.JobSnapshot()).find((job) => !before.has(job.id)));
  };
  try {
    await wait('native Wails bindings', () => window.go?.webgui?.App && window.runtime?.EventsOn);
    const api = window.go.webgui.App;
    window.runtime.EventsOn('__dragfm_native_input_result__', (value) => { const result = JSON.parse(value); const done = inputReplies.get(result.id); inputReplies.delete(result.id); done?.(result); });
    window.runtime.WindowSetSize(1080, 680);
    window.runtime.WindowSetPosition(20, 40);
    await wait('bounded native window', () => window.innerWidth <= 1100);
    window.runtime.EventsOn('terminal:cwd', (event) => { const previous = cwd[event.pane]; if (!previous || previous.session !== event.session || (event.sequence || 0) > (previous.sequence || 0)) cwd[event.pane] = event; wake(); });
    window.runtime.EventsOn('terminal:data', (event) => { terminalOutput[event.pane] = ((terminalOutput[event.pane] || '') + atob(event.data)).slice(-32768); wake(); });
    window.runtime.EventsOn('job:update', (event) => { events.push(event); if (events.length > 2000) events.shift(); wake(); });
    window.runtime.EventsOn('challenge', (event) => {
      void (async () => {
        if (event.kind === 'password' && ['目标端提权 · 本机', '目标目录需要管理员权限'].includes(event.title) && event.message.includes(plan.protected)) {
          await click(declineLocalSudo ? (event.allowSkip ? '取消任务' : '取消') : '继续');
          return;
        }
        if (event.kind !== 'confirm-host-key' || !event.message.includes(plan.fingerprint) || plan.phase === 'changed-key') {
          challengeFailure = 'unexpected SSH authentication challenge';
          await api.ResolveChallenge(event.id, false, '', false);
          return;
        }
        await click('继续');
      })().catch((error) => { challengeFailure = String(error); wake(); });
    });
    const status = await api.VaultStatus();
    assert(status.exists === (plan.phase !== 'exercise'), 'wrong isolated vault state');
    checks.push('production Wails RPC available; vault starts locked');
    await wait('unlock form', () => document.querySelector('.unlock-panel form'));
    const passwords = document.querySelectorAll('.unlock-panel input[type=password]');
    input(passwords[0], plan.password);
    if (plan.phase === 'exercise') {
      input(document.querySelector('.unlock-panel input:not([type=password])'), 'Disposable native acceptance fixture');
      input(passwords[1], plan.password);
    }
    await pause(40);
    document.querySelector('.unlock-panel form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await wait('native workspace', () => document.querySelector('.workspace-grid'));
    checks.push('native unlock/create form and actual encrypted vault');
    if (plan.phase !== 'exercise') {
      const restored = await api.Bootstrap();
      assert(plan.historyIDs.every((id) => restored.history.some((entry) => entry.id === id)), 'history missing after actual process restart');
      assert((await api.JobSnapshot()).length === 0, 'pending work restored after process restart');
      await wait('restored History visible', () => document.querySelectorAll('.history-row').length >= plan.historyIDs.length);
      checks.push('history restored in a new native process; no pending work restored');
      if (plan.phase === 'changed-key') {
        await wait('changed SSH host key blocked', () => [...document.querySelectorAll('.pane-error')].some((item) => /fingerprint|host.key|指纹/i.test(item.textContent)));
        checks.push('changed SSH host key blocks native reconnection');
      } else {
        await wait('saved SSH route and PTY reconnect', () => cwd.right && pane('right').querySelector('select').value === 'Native SSH');
        checks.push('saved Markdown private-key route reconnects without new TOFU');
      }
    } else {
      await wait('initial local PTYs', () => cwd.left && cwd.right);
      await click('连接配置');
      const editor = await wait('CodeMirror configuration editor', () => document.querySelector('.cm-content'));
      editor.focus();
      const selection = window.getSelection(), range = document.createRange();
      range.selectNodeContents(editor); selection.removeAllRanges(); selection.addRange(range);
      assert(document.execCommand('insertText', false, plan.markdown), 'native CodeMirror text insertion failed');
      await pause(150);
      await click('显示密码');
      await wait('explicitly revealed password', () => document.querySelector('.cm-content').textContent.includes('fixture-secret'));
      await click('隐藏密码');
      await wait('password re-masked', () => !document.querySelector('.cm-content').textContent.includes('fixture-secret'));
      assert(document.querySelectorAll('.cm-lineNumbers .cm-gutterElement').length > 2, 'physical line numbers missing');
      await click('验证并保存');
      await wait('configuration saved', () => !document.querySelector('.config-markdown-layout'));
      const config = await api.GetConfigTexts();
      assert(config.markdown.includes('Native SSH') && !config.markdown.includes('\n\n'), 'configuration draft was lost or blank lines retained');
      checks.push('native CodeMirror editing, physical line numbers, mask toggle and strict Markdown save');
      await click('连接配置');
      await click('连接、池与缓存');
      const connectionRow = await wait('saved connection status row', () => document.querySelector('.connection-row[aria-label="Native SSH"]'));
      const footer = document.querySelector('.modal-footer').getBoundingClientRect();
      const dialog = document.querySelector('.modal').getBoundingClientRect();
      assert(footer.bottom <= dialog.bottom + 1 && dialog.bottom <= window.innerHeight + 1, 'configuration controls overflow the native viewport');
      await click('仅测试此会话', connectionRow);
      await wait('selected SSH login shown in configuration', () => connectionRow.querySelector('.connection-result.succeeded'));
      assert(connectionRow.querySelector('.connection-result').textContent.includes('不是 TCP RTT'), 'connection test is presented as remote transfer or latency evidence');
      const allowRelay = [...connectionRow.querySelectorAll('label')].find((item) => item.textContent.includes('允许作跳板')).querySelector('input');
      allowRelay.click();
      await wait('endpoint-only policy saved and visible', () => !allowRelay.checked && !allowRelay.disabled && connectionRow.textContent.includes('仅端点，不作跳板'));
      assert((await api.GetConfigTexts()).markdown.includes('###允许跳板\nfalse'), 'relay opt-out did not reach persistent Markdown');
      await click('文本配置');
      await wait('policy change appears in physical-line editor', () => document.querySelector('.cm-content').textContent.includes('允许跳板'));
      await click('验证并保存');
      await wait('policy draft can save without stale revision', () => !document.querySelector('.config-markdown-layout'));
      checks.push('production configuration controls → selected real SSH login → relay opt-out → Markdown resave; DOM input, not OS-input proof');
      const previous = cwd.right.session;
      input(pane('right').querySelector('select'), 'Native SSH');
      await wait('SSH endpoint selected', () => pane('right').querySelector('select').value === 'Native SSH');
      await wait('remote PTY started', () => cwd.right?.session !== previous);
      await navigate('left', plan.source);
      await navigate('right', plan.target);
      await nativeClick(pathInput('left'));
      await nativeKeys('command', 'right');
      for (let count = 0; count < 'source'.length; count++) await nativeKeys('shift', 'left');
      const pathField = pathInput('left');
      assert(pathField.value.slice(pathField.selectionStart, pathField.selectionEnd) === 'source', 'OS partial path selection is wrong');
      await nativeText('target');
      await wait('partial path replacement', () => pathField.value === plan.target);
      await nativeText('/');
      await nativeKeys('backspace');
      assert(pathField.value === plan.target && cwd.left.path === plan.source, 'path-input Backspace navigated instead of editing text');
      await nativeKeys('enter');
      await wait('OS path Enter navigation', () => cwd.left.path === plan.target);
      await navigate('left', plan.source);
      checks.push('OS partial path selection/replacement, input Backspace and Enter; trusted keyboard events');
      await nativeClick(pane('left').querySelector('.xterm-screen'));
      await nativeText('native_profile_probe');
      await nativeKeys('enter');
      await wait('actual zsh profile function and variable', () => terminalOutput.left.includes('\r\nPROFILE=profile-loaded\r\n'));
      const lastRenderedLine = (which) => [...pane(which).querySelectorAll('.xterm-rows > div')].map((item) => item.textContent.replaceAll('\u00a0', ' ').trimEnd()).filter(Boolean).at(-1);
      await wait('rendered complete zsh prompt', () => lastRenderedLine('left') === 'NATIVE>');
      checks.push('OS terminal input executes actual zprofile variable/zshrc function; complete rendered prompt');
      await nativeClick(pane('left').querySelector('.file-viewport'), true);
      for (const theme of ['light', 'dark', 'system']) {
        await nativeKeys('ctrl', 'shift', 't');
        await wait(`OS theme ${theme}`, () => document.querySelector('.workspace').dataset.theme === theme);
        await native('capture', { label: `theme-${theme}` });
      }
      checks.push('OS Ctrl+Shift+T cycles all three themes; per-theme native window screenshots');
      for (const which of ['left', 'right']) {
        const listing = await api.List(which, pane(which).querySelector('select').value, pathInput(which).value);
        assert(listing.entries.every((entry, index, all) => index === 0 || Date.parse(all[index-1].modified) >= Date.parse(entry.modified)), 'file listing is not sorted by newest modification time');
        assert(rows(which).every((element, index) => element.querySelector('.file-name').getAttribute('title') === listing.entries[index]?.name), 'rendered file order differs from endpoint listing');
      }
      checks.push('real SSH TOFU, key authentication, browsing and file-to-shell cwd');
      checks.push('local and SSH file lists render newest-mtime-first order');
      await browseMany('left', plan.source);
      await browseMany('right', plan.target);
      checks.push('OS wheel in local and SSH multi-screen file lists; deep file selection/metadata; deep directory double-click and Backspace with PTY/scroll reset');
      const first = await drag('first.bin', '复制');
      await wait('first transfer running', async () => (await api.JobSnapshot()).some((item) => item.id === first.id && item.state === 'running'));
      const second = await drag('second.txt', '复制');
      await wait('Running and Pending simultaneously visible', () => document.querySelector('.running-line strong')?.textContent.includes('first.bin') && document.querySelector('.pending-list')?.textContent.includes('second.txt'));
      assert((await api.JobSnapshot()).filter((item) => item.state === 'running').length === 1, 'more than one job running');
      await native('capture', { label: 'running-and-pending' });
      checks.push('OS directory drag/drop and one Running plus one Pending visible together');
      const pending = await queueCommand('sleep 30; printf pending-should-not-run');
      // Match the sanitized command preview returned by the actual backend,
      // including its selected endpoint, rather than a generic command label.
      const findPendingRow = () => [...document.querySelectorAll('.pending-list .queue-row')].find((item) => item.querySelector('span')?.textContent === pending.description);
      const pendingRow = await wait('pending command rendered', findPendingRow);
      let pendingCancel = pendingRow.querySelector('button');
      const list = document.querySelector('.pending-list');
      const visibility = () => {
        const location = point(pendingCancel);
        return { clientHeight: list.clientHeight, scrollHeight: list.scrollHeight, scrollTop: list.scrollTop,
          point: location, hit: pendingCancel.contains(document.elementFromPoint(location.x, location.y)) };
      };
      pendingCancellation.id = pending.id;
      pendingCancellation.beforeScroll = visibility();
      const pendingPoint = point(pendingCancel);
      if (!pendingCancel.contains(document.elementFromPoint(pendingPoint.x, pendingPoint.y))) {
        // At the minimum window size only one Pending row fits. Scroll the
        // actual queue with an OS wheel event before clicking its second row.
        const wheelPoint = point(list), wheels = trusted.wheel;
        assert(list.contains(document.elementFromPoint(wheelPoint.x, wheelPoint.y)), 'Pending wheel target is covered');
        await native('scroll', { point: wheelPoint, clicks: -3 });
        await wait('OS scroll exposes pending cancellation', () => {
          const location = point(pendingCancel);
          return trusted.wheel > wheels && pendingRow.isConnected && pendingCancel.contains(document.elementFromPoint(location.x, location.y));
        });
      }
      pendingCancellation.stateBeforeClick = (await api.JobSnapshot()).find((item) => item.id === pending.id)?.state;
      assert(pendingCancellation.stateBeforeClick === 'pending', 'cancel target is no longer Pending; cannot count this as queued cancellation');
      pendingCancel = findPendingRow()?.querySelector('button');
      pendingCancellation.beforeClick = visibility();
      await nativeClick(pendingCancel);
      pendingCancellation.trustedClick = true;
      await complete(pending.id, 'cancelled');
      checks.push('OS click reaches the visible pending cancel control; queued command is cancelled');
      const firstDone = await complete(first.id);
      await complete(second.id);
      assert(firstDone.bytesDone === plan.firstBytes && firstDone.bytesTotal === plan.firstBytes, 'final byte counters do not match actual file size');
      await wait('completed jobs visible in History', () => document.querySelector('.history-list').textContent.includes('first.bin') && document.querySelector('.history-list').textContent.includes('second.txt'));
      checks.push('real transfers completed, actual byte counts and native History rendered');
      const failed = await queueCommand('printf native-failure; exit 19');
      await complete(failed.id, 'failed');
      const running = await queueCommand('sleep 30; printf running-should-not-finish');
      await wait('command running', async () => (await api.JobSnapshot()).some((job) => job.id === running.id && job.state === 'running'));
      const cancelStarted = Date.now();
      await nativeClick(await wait('running cancellation control rendered', () => document.querySelector('.running-line button')));
      await complete(running.id, 'cancelled');
      assert(Date.now() - cancelStarted < 5000, 'SSH cancellation took longer than five seconds');
      checks.push('native pending/running cancellation and failed SSH command');
      await drag('second.txt', '移动并覆盖').then((job) => complete(job.id));
      await wait('moved source disappeared', () => !row('left', 'second.txt'));
      checks.push('native overwrite confirmation and hash-verified cross-endpoint move');
      await drag('tree', '复制并覆盖').then((job) => complete(job.id));
      checks.push('native directory overwrite uses merge semantics');
      await nativeClick(pane('left').querySelector('button[title="刷新"]'));
      await wait('unselected refreshed file list', () => !pane('left').querySelector('.loading-line') && !pane('left').querySelector('.selected'));
      await nativeClick(pane('left').querySelector('.file-viewport'), true);
      await wait('left pane focused', () => pane('left').classList.contains('active'));
      assert(!pane('left').querySelector('.selected'), 'blank-area focus unexpectedly selected a row');
      await nativeKeys('backspace');
      await wait('Backspace without selection', () => pathInput('left').value === plan.parent);
      await wait('Backspace shell synchronized', () => cwd.left.path === plan.parent);
      await navigate('left', plan.source);
      checks.push('OS Backspace navigates with no selected row, including native Edit menu routing');
      const hashRow = await wait('hashable file', () => row('left', 'hash.txt'));
      await nativeClick(hashRow);
      await wait('hash row selected', () => hashRow.classList.contains('selected'));
      await nativeKeys('h');
      await wait('SHA-256 hotkey output', async () => (await api.JobSnapshot()).some((job) => job.state === 'succeeded' && job.message.includes(plan.hash)));
      checks.push('native h hotkey returns real SHA-256');
      const removeRow = await wait('deletable file', () => row('left', 'delete.txt'));
      await nativeClick(removeRow);
      await wait('delete row selected', () => removeRow.classList.contains('selected'));
      await nativeKeys('d');
      await wait('delete confirmation', () => document.querySelector('.delete-confirm'));
      await nativeClick(document.querySelector('.danger-button'));
      await wait('deleted file disappears', () => !row('left', 'delete.txt'));
      checks.push('native d hotkey, confirmation, deletion and refresh');
      await nativeClick(pane('right').querySelector('.xterm-screen'));
      const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
      await nativeText(`cd ${quote(plan.target + '/archive')}; printf NATIVE_INPUT=okX`);
      await nativeKeys('backspace');
      await nativeText("; printf '\\n'");
      await nativeKeys('enter');
      await wait('persistent SSH PTY receives edited OS input', () => terminalOutput.right.includes('\r\nNATIVE_INPUT=ok\r\n'));
      await wait('shell-to-file cwd', () => pathInput('right').value === `${plan.target}/archive`);
      assert(!terminalOutput.right.includes('\x1b]777;dragfm-cwd='), 'internal cwd markers leaked to xterm');
      checks.push('OS xterm typing/Backspace/Enter, real SSH execution and shell-to-file cwd');
      await navigate('left', plan.protected);
      const protectedCopy = await drag('second.txt', '复制', null, 'right', 'left', plan.protected);
      await complete(protectedCopy.id);
      checks.push('native remote-to-protected-local download through real scoped sudo');
      declineLocalSudo = true;
      const refusedMove = await drag('second.txt', '移动并覆盖', null, 'right', 'left', plan.protected);
      // The product now distinguishes cancellation from transfer failure.
      // Keep source-retention verification; do not expect an obsolete state.
      await complete(refusedMove.id, 'cancelled');
      assert(row('right', 'second.txt'), 'declined sudo move removed source');
      checks.push('declined local sudo prevents move and retains remote source');
      await navigate('left', plan.source);
      await wait('prompt after repeated navigation', () => lastRenderedLine('left') === 'NATIVE>');
      window.runtime.WindowSetSize(1080, 680);
      await wait('small native window', () => window.innerWidth <= 1100);
      const tasks = document.querySelector('.task-pane').getBoundingClientRect();
      assert(tasks.right <= window.innerWidth + 1 && tasks.bottom <= window.innerHeight + 1, 'task pane overflows native window');
      for (const control of document.querySelectorAll('.task-header, .running-block, .output-tabs, .queue-section > header, .command-bar')) {
        const rect = control.getBoundingClientRect();
        assert(rect.height > 0 && rect.top >= tasks.top && rect.bottom <= tasks.bottom + 1 && rect.right <= innerWidth + 1, 'third-column control is clipped or inaccessible');
      }
      await nativeClick(document.querySelector('.history-row'));
      await wait('history detail opens', () => document.querySelector('.output-detail'));
      await native('capture', { label: 'history-and-terminal' });
      checks.push('minimum-size third-column controls are reachable; OS history click; complete prompt after repeated cwd changes');
    }
    const jobs = await window.go.webgui.App.JobSnapshot();
    const history = (await window.go.webgui.App.Bootstrap()).history;
    assert(events.every((event) => !event.description?.includes(plan.password) && !event.message?.includes(plan.password)), 'vault password leaked in events');
    window.runtime.EventsEmit('__dragfm_native_smoke_result__', JSON.stringify({ success: true, phase: plan.phase, checks, trustedInput: trusted, fileBrowsing, pendingCancellation, historyIDs: history.map((job) => job.id), jobs: jobs.map(({ id, state, bytesDone, method }) => ({ id, state, bytesDone, method })) }));
  } catch (error) {
    // This exact stage is past the closed configuration editor and only shows
    // disposable paths. Other failures may expose keys: never capture those.
    if (stage === 'partial path replacement' && !document.querySelector('.modal-backdrop') &&
        document.activeElement?.matches('.path-form input')) {
      try { await native('capture', { label: 'path-input-failure' }); } catch { /* retain the original failure */ }
    }
    let message = String(error);
    if (plan?.password) message = message.replaceAll(plan.password, '[redacted]');
    message = message.replace(/-----BEGIN [\s\S]*?PRIVATE KEY-----[\s\S]*?-----END [\s\S]*?PRIVATE KEY-----/g, '[private key redacted]');
    window.runtime?.EventsEmit('__dragfm_native_smoke_result__', JSON.stringify({ success: false, phase: plan?.phase, stage, checks, error: message, trustedInput: trusted, fileBrowsing, pendingCancellation, terminalTail: {left: terminalOutput.left?.slice(-3000), right: terminalOutput.right?.slice(-3000)}, leftPath: pathInput('left')?.value, rightPath: pathInput('right')?.value, leftCWD: cwd.left?.path, rightCWD: cwd.right?.path, active: document.querySelector('.file-pane.active')?.dataset.pane, modalCount: document.querySelectorAll('.modal-backdrop').length }));
  }
})();
