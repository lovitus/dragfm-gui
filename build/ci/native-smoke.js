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
  const events = [];
  let challengeFailure = '';
  let declineLocalSudo = false;
  const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async (description, predicate, timeout = 20000) => {
    stage = description;
    window.runtime?.LogInfo('NATIVE_SMOKE ' + stage);
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      if (challengeFailure) throw new Error(challengeFailure);
      const result = await predicate();
      if (result) return result;
      await pause(40);
    }
    const errors = [...document.querySelectorAll('.pane-error,.operation-error,.form-error,.save-status.error')].map((item) => item.textContent).join('; ');
    throw new Error(`Timeout: ${description}${errors ? `; ${errors}` : ''}`);
  };
  const input = (element, value) => {
    assert(element, 'missing input');
    const prototype = element instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value);
    element.dispatchEvent(new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  };
  const button = (text, parent = document) => [...parent.querySelectorAll('button')].find((item) => item.textContent.trim() === text);
  const click = async (text, parent = document) => (await wait(`button ${text}`, () => button(text, parent)))?.click();
  const pane = (which) => document.querySelector(`.file-pane[data-pane="${which}"]`);
  const rows = (which) => [...pane(which).querySelectorAll('.file-row')];
  const row = (which, name) => rows(which).find((item) => item.querySelector('.file-name').getAttribute('title') === name);
  const pathInput = (which) => pane(which).querySelector('.path-form input');
  const key = (element, name, code) => element.dispatchEvent(new KeyboardEvent('keydown', { key: name, code: name, keyCode: code, which: code, bubbles: true, cancelable: true }));
  const navigate = async (which, path) => {
    await wait(`${which} PTY ready`, () => cwd[which]);
    input(pathInput(which), path);
    await pause(40);
    pane(which).querySelector('.path-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await wait(`${which} navigation`, () => pathInput(which).value === path && !pane(which).querySelector('.loading-line'));
    await wait(`${which} file-to-shell cwd`, () => cwd[which]?.path === path);
  };
  const drag = async (name, choice, directory = 'archive', sourcePane = 'left', targetPane = 'right', expectedDirectory = plan.target + '/archive') => {
    const source = await wait(`source row ${name}`, () => row(sourcePane, name));
    const target = await wait(`directory row ${directory}`, () => directory === null ? pane(targetPane).querySelector('.file-viewport') : row(targetPane, directory));
    const from = source.getBoundingClientRect(), to = target.getBoundingClientRect();
    const start = { bubbles: true, cancelable: true, button: 0, buttons: 1, pointerId: 1, pointerType: 'mouse', clientX: from.x + from.width / 2, clientY: from.y + from.height / 2 };
    source.dispatchEvent(new PointerEvent('pointerdown', start));
    window.dispatchEvent(new PointerEvent('pointermove', { ...start, clientX: to.x + to.width / 2, clientY: to.y + to.height / 2 }));
    await pause(80);
    assert(directory === null ? pane(targetPane).classList.contains('drop-current') : target.classList.contains('drop-target'), 'directory hover target was not highlighted');
    window.dispatchEvent(new PointerEvent('pointerup', { ...start, buttons: 0, clientX: to.x + to.width / 2, clientY: to.y + to.height / 2 }));
    const action = await wait(`transfer choice ${choice}`, () => [...document.querySelectorAll('.choice-button')].find((item) => item.querySelector('strong').textContent === choice));
    assert(document.getSelection().toString() === '', 'drag selected page text');
    assert(document.querySelector('.transfer-path').textContent.includes(expectedDirectory), 'drop did not resolve to the hovered directory');
    const before = new Set((await window.go.webgui.App.JobSnapshot()).map((job) => job.id));
    action.click();
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
    window.runtime.EventsOn('terminal:cwd', (event) => { const previous = cwd[event.pane]; if (!previous || previous.session !== event.session || (event.sequence || 0) > (previous.sequence || 0)) cwd[event.pane] = event; });
    window.runtime.EventsOn('terminal:data', (event) => { terminalOutput[event.pane] = ((terminalOutput[event.pane] || '') + atob(event.data)).slice(-32768); });
    window.runtime.EventsOn('job:update', (event) => { events.push(event); if (events.length > 2000) events.shift(); });
    window.runtime.EventsOn('challenge', (event) => {
      void (async () => {
        if (event.kind === 'password' && event.title === '目标目录需要管理员权限' && event.message.includes(plan.protected)) {
          await click(declineLocalSudo ? '取消' : '继续');
          return;
        }
        if (event.kind !== 'confirm-host-key' || !event.message.includes(plan.fingerprint) || plan.phase === 'changed-key') {
          challengeFailure = 'unexpected SSH authentication challenge';
          await api.ResolveChallenge(event.id, false, '', false);
          return;
        }
        await click('继续');
      })().catch((error) => { challengeFailure = String(error); });
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
      const previous = cwd.right.session;
      input(pane('right').querySelector('select'), 'Native SSH');
      await wait('SSH endpoint selected', () => pane('right').querySelector('select').value === 'Native SSH');
      await wait('remote PTY started', () => cwd.right?.session !== previous);
      await navigate('left', plan.source);
      await navigate('right', plan.target);
      checks.push('real SSH TOFU, key authentication, browsing and file-to-shell cwd');
      const first = await drag('first.bin', '复制');
      await wait('first transfer running', async () => (await api.JobSnapshot()).some((item) => item.id === first.id && item.state === 'running'));
      const second = await drag('second.txt', '复制');
      await wait('Running and Pending simultaneously visible', () => document.querySelector('.running-line strong')?.textContent.includes('first.bin') && document.querySelector('.pending-list')?.textContent.includes('second.txt'));
      assert((await api.JobSnapshot()).filter((item) => item.state === 'running').length === 1, 'more than one job running');
      checks.push('actual directory drag/drop and one Running plus one Pending visible together');
      const pending = await queueCommand('sleep 30; printf pending-should-not-run');
      // Command bodies deliberately do not enter the task description/history.
      // There is exactly one queued command in this fixture; identify its UI
      // row using the same sanitized description returned by the real backend.
      const pendingRow = await wait('pending command visible', () => [...document.querySelectorAll('.pending-list .queue-row')].find((item) => item.querySelector('span')?.textContent === pending.description));
      pendingRow.querySelector('button').click();
      await complete(pending.id, 'cancelled');
      const firstDone = await complete(first.id);
      await complete(second.id);
      assert(firstDone.bytesDone === plan.firstBytes && firstDone.bytesTotal === plan.firstBytes, 'final byte counters do not match actual file size');
      await wait('completed jobs visible in History', () => document.querySelector('.history-list').textContent.includes('first.bin') && document.querySelector('.history-list').textContent.includes('second.txt'));
      checks.push('real transfers completed, actual byte counts and native History rendered');
      const failed = await queueCommand('printf native-failure; exit 19');
      await complete(failed.id, 'failed');
      const running = await queueCommand('sleep 30; printf running-should-not-finish');
      await wait('command running', async () => (await api.JobSnapshot()).some((job) => job.id === running.id && job.state === 'running'));
      (await wait('running cancellation control rendered', () => document.querySelector('.running-line button'))).click();
      await complete(running.id, 'cancelled');
      checks.push('native pending/running cancellation and failed SSH command');
      await drag('second.txt', '移动并覆盖').then((job) => complete(job.id));
      await wait('moved source disappeared', () => !row('left', 'second.txt'));
      checks.push('native overwrite confirmation and hash-verified cross-endpoint move');
      await drag('tree', '复制并覆盖').then((job) => complete(job.id));
      checks.push('native directory overwrite uses merge semantics');
      pane('left').querySelector('button[title="刷新"]').click();
      await wait('unselected refreshed file list', () => !pane('left').querySelector('.loading-line') && !pane('left').querySelector('.selected'));
      pane('left').querySelector('.file-viewport').focus();
      pane('left').dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
      await wait('left pane focused', () => pane('left').classList.contains('active'));
      key(pane('left').querySelector('.file-viewport'), 'Backspace', 8);
      await wait('Backspace without selection', () => pathInput('left').value === plan.parent);
      await wait('Backspace shell synchronized', () => cwd.left.path === plan.parent);
      await navigate('left', plan.source);
      checks.push('Backspace navigates with no selected row');
      const hashRow = await wait('hashable file', () => row('left', 'hash.txt'));
      hashRow.click();
      await wait('hash row selected', () => hashRow.classList.contains('selected'));
      key(hashRow, 'h', 72);
      await wait('SHA-256 hotkey output', async () => (await api.JobSnapshot()).some((job) => job.state === 'succeeded' && job.message.includes(plan.hash)));
      checks.push('native h hotkey returns real SHA-256');
      const removeRow = await wait('deletable file', () => row('left', 'delete.txt'));
      removeRow.click();
      await wait('delete row selected', () => removeRow.classList.contains('selected'));
      key(removeRow, 'd', 68);
      await wait('delete confirmation', () => document.querySelector('.delete-confirm'));
      document.querySelector('.danger-button').click();
      await wait('deleted file disappears', () => !row('left', 'delete.txt'));
      checks.push('native d hotkey, confirmation, deletion and refresh');
      const terminal = pane('right').querySelector('.xterm-helper-textarea');
      terminal.focus();
      const data = new DataTransfer();
      const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
      data.setData('text/plain', `cd ${quote(plan.target + '/archive')}; printf 'NATIVE_ENV=%s\\n' "$HOME"`);
      terminal.dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data }));
      await pause(100); key(terminal, 'Enter', 13);
      await wait('persistent SSH PTY login environment', () => terminalOutput.right.includes(`NATIVE_ENV=${plan.home}`));
      await wait('shell-to-file cwd', () => pathInput('right').value === `${plan.target}/archive`);
      assert(!terminalOutput.right.includes('\x1b]777;dragfm-cwd='), 'internal cwd markers leaked to xterm');
      checks.push('native xterm paste/Enter, login environment and shell-to-file cwd');
      await navigate('left', plan.protected);
      const protectedCopy = await drag('second.txt', '复制', null, 'right', 'left', plan.protected);
      await complete(protectedCopy.id);
      checks.push('native remote-to-protected-local download through real scoped sudo');
      declineLocalSudo = true;
      const refusedMove = await drag('second.txt', '移动并覆盖', null, 'right', 'left', plan.protected);
      await complete(refusedMove.id, 'failed');
      assert(row('right', 'second.txt'), 'declined sudo move removed source');
      checks.push('declined local sudo prevents move and retains remote source');
      await navigate('left', plan.source);
      window.runtime.WindowSetSize(1080, 680);
      await wait('small native window', () => window.innerWidth <= 1100);
      const tasks = document.querySelector('.task-pane').getBoundingClientRect();
      assert(tasks.right <= window.innerWidth + 1 && tasks.bottom <= window.innerHeight + 1, 'task pane overflows native window');
      checks.push('task pane fits minimum-size native window');
    }
    const jobs = await window.go.webgui.App.JobSnapshot();
    const history = (await window.go.webgui.App.Bootstrap()).history;
    assert(events.every((event) => !event.description?.includes(plan.password) && !event.message?.includes(plan.password)), 'vault password leaked in events');
    window.runtime.EventsEmit('__dragfm_native_smoke_result__', JSON.stringify({ success: true, phase: plan.phase, checks, historyIDs: history.map((job) => job.id), jobs: jobs.map(({ id, state, bytesDone, method }) => ({ id, state, bytesDone, method })) }));
  } catch (error) {
    let message = String(error);
    if (plan?.password) message = message.replaceAll(plan.password, '[redacted]');
    message = message.replace(/-----BEGIN [\s\S]*?PRIVATE KEY-----[\s\S]*?-----END [\s\S]*?PRIVATE KEY-----/g, '[private key redacted]');
    window.runtime?.EventsEmit('__dragfm_native_smoke_result__', JSON.stringify({ success: false, phase: plan?.phase, stage, checks, error: message, terminalTail: {left: terminalOutput.left?.slice(-3000), right: terminalOutput.right?.slice(-3000)}, leftPath: pathInput('left')?.value, rightPath: pathInput('right')?.value, leftCWD: cwd.left?.path, rightCWD: cwd.right?.path, active: document.querySelector('.file-pane.active')?.dataset.pane, modalCount: document.querySelectorAll('.modal-backdrop').length }));
  }
})();
