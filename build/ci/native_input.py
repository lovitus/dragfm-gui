"""OS input adapter for the existing hosted macOS Wails acceptance fixture.

PyAutoGUI posts Quartz events. DOM is used only to locate/assert UI state.
No network listener, permission edits, fallback dispatchEvent, or user device.
"""
from contextlib import contextmanager
import json
import math
import os
import re
import subprocess
import threading
import time


class NativeInput:
    def __init__(self, evidence):
        if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP'):
            raise RuntimeError('OS input is restricted to the disposable hosted runner')
        import AppKit
        import ApplicationServices
        import Quartz
        import pyautogui
        self.appkit, self.quartz, self.ui = AppKit, Quartz, pyautogui
        self.ax = ApplicationServices
        self.evidence = evidence
        self.down = False
        self.counts = {}
        self.original_display_mode = None
        if not Quartz.CGPreflightPostEventAccess():
            raise RuntimeError('BLOCKER: hosted runner has no macOS event-posting permission; OS input is unverified')
        if not self.ax.AXIsProcessTrusted():
            raise RuntimeError('BLOCKER: hosted input process has no accessibility permission; no prompt or bypass attempted')
        self.system_focus = self.ax.AXUIElementCreateSystemWide()
        # A system-wide element sets this Python process's AX timeout, not an
        # OS permission or a timeout local to this one element.
        status = self.ax.AXUIElementSetMessagingTimeout(self.system_focus, 2.0)
        if status != self.ax.kAXErrorSuccess:
            raise RuntimeError(f'native focus timeout configuration failed (AX {status})')
        # Keep PyAutoGUI's corner fail-safe enabled. No TCC changes or prompts.
        self.ui.PAUSE = 0.08
        # Retain the existing cadence while correcting the independently
        # observed special-key flags. Slower delivery did not fix that defect.
        self.ui.DARWIN_CATCH_UP_TIME = 0.05

    @contextmanager
    def text_key_events(self):
        # cd3042a's actual Quartz/AppKit evidence showed ordinary letters sent
        # with Fn+NumericPad after path-selection arrows. PyAutoGUI passes a
        # null event source, without setting flags. Remove ONLY those two
        # unintended special-key bits, not legitimate Shift/Cmd/Ctrl/Alt.
        # Only the printable-text action enters here, never arrows or chords.
        # No product/user event is intercepted.
        quartz = self.quartz
        original_post = quartz.CGEventPost
        special = quartz.kCGEventFlagMaskSecondaryFn | quartz.kCGEventFlagMaskNumericPad

        def post(tap, event):
            if quartz.CGEventGetType(event) in (quartz.kCGEventKeyDown, quartz.kCGEventKeyUp, quartz.kCGEventFlagsChanged):
                flags = quartz.CGEventGetFlags(event)
                if flags & special:
                    quartz.CGEventSetFlags(event, flags & ~special)
                    self.counts['special-key-flags-removed'] = self.counts.get('special-key-flags-removed', 0) + 1
                if quartz.CGEventGetFlags(event) & special:
                    raise RuntimeError('plain text event still carries unintended special-key flags')
            return original_post(tap, event)

        # The parent serializes actions. Restore even on fail-safe/input error;
        # no raw key/character logging or process-lifetime monkey patch.
        quartz.CGEventPost = post
        try:
            yield
        finally:
            quartz.CGEventPost = original_post

    def prepare_display(self):
        # macOS hosted VMs may start at 1024x768, narrower than the product's
        # minimum window. Select only a mode CoreGraphics says is supported;
        # never resize the product below its minimum or post clipped input.
        display = self.quartz.CGMainDisplayID()
        bounds = self.quartz.CGDisplayBounds(display)
        report = dict(original=[bounds.size.width, bounds.size.height], changed=False)
        try:
            if bounds.size.width >= 1280 and bounds.size.height >= 800:
                return
            modes = self.quartz.CGDisplayCopyAllDisplayModes(display, None) or []
            dimensions = lambda mode: (self.quartz.CGDisplayModeGetWidth(mode),
                                       self.quartz.CGDisplayModeGetHeight(mode))
            report['available'] = sorted(set(dimensions(mode) for mode in modes))
            candidates = [mode for mode in modes if dimensions(mode)[0] >= 1280
                          and dimensions(mode)[1] >= 800
                          and self.quartz.CGDisplayModeIsUsableForDesktopGUI(mode)]
            if not candidates:
                raise RuntimeError('BLOCKER: hosted display has no supported desktop mode of at least 1280x800')
            selected = min(candidates, key=lambda mode: dimensions(mode)[0] * dimensions(mode)[1])
            original = self.quartz.CGDisplayCopyDisplayMode(display)
            if original is None:
                raise RuntimeError('BLOCKER: hosted original display mode could not be retained')
            # Synchronous, process-lifetime-only change per CoreGraphics. Keep
            # the original CF object alive for explicit restoration in finally.
            status = self.quartz.CGDisplaySetDisplayMode(display, selected, None)
            if status != self.quartz.kCGErrorSuccess:
                raise RuntimeError(f'BLOCKER: hosted supported display-mode switch failed ({status})')
            self.original_display_mode = (display, original)
            report['changed'] = True
            bounds = self.quartz.CGDisplayBounds(display)
            report['actual'] = [bounds.size.width, bounds.size.height]
            if bounds.size.width < 1280 or bounds.size.height < 800:
                raise RuntimeError('BLOCKER: selected mode still has insufficient logical desktop bounds')
        except Exception as error:
            report['error'] = str(error)
            raise
        finally:
            (self.evidence / 'DISPLAY_MODE.json').write_text(json.dumps(report, indent=2) + '\n')

    def restore_display(self):
        if self.original_display_mode is None:
            return
        display, original = self.original_display_mode
        status = self.quartz.CGDisplaySetDisplayMode(display, original, None)
        (self.evidence / 'DISPLAY_RESTORED.json').write_text(json.dumps(dict(status=status)) + '\n')
        if status != self.quartz.kCGErrorSuccess:
            raise RuntimeError(f'hosted display restoration failed ({status})')
        self.original_display_mode = None

    def perform(self, process, request):
        if process.poll() is not None:
            raise RuntimeError('native input target process exited')
        activated = False
        if self.focused_pid() != process.pid:
            # NS is needed only to request activation. A live AX keyboard
            # recipient does not need an unused NS activation object to exist.
            # Missing AX identity still fails; NS never substitutes for it.
            application = self.appkit.NSRunningApplication.runningApplicationWithProcessIdentifier_(process.pid)
            if application is None:
                raise RuntimeError('native acceptance application is unavailable for activation')
            activated = True
            application.activateWithOptions_(self.appkit.NSApplicationActivateIgnoringOtherApps)
            time.sleep(0.15)  # Event delivery, not a status-poll loop.
        windows = self.quartz.CGWindowListCopyWindowInfo(self.quartz.kCGWindowListOptionOnScreenOnly, self.quartz.kCGNullWindowID)
        windows = [w for w in windows if w.get('kCGWindowOwnerPID') == process.pid
                   and w.get('kCGWindowLayer') == 0 and w['kCGWindowBounds']['Width'] >= 1080]
        if len(windows) != 1:
            raise RuntimeError('native input needs one identifiable product window')
        window, viewport = windows[0], request['viewport']
        frame = window['kCGWindowBounds']
        width, height = viewport['width'], viewport['height']
        # Window bounds and CGEvent positions are logical points, not Retina
        # framebuffer pixels. Do not use pixel dimensions as clipping proof.
        screen = self.quartz.CGDisplayBounds(self.quartz.CGMainDisplayID())
        screen_width, screen_height = screen.size.width, screen.size.height
        if (abs(frame['Width'] - width) > 4 or not 0 <= frame['Height'] - height <= 80
                or frame['X'] < 0 or frame['Y'] < 0
                or frame['X'] + frame['Width'] > screen_width
                or frame['Y'] + frame['Height'] > screen_height):
            raise RuntimeError('BLOCKER: product window/viewport is clipped or coordinate mapping is ambiguous: '
                               f'frame={dict(frame)} viewport={width}x{height} screen={screen_width}x{screen_height}')

        def point():
            x, y = request['point']['x'], request['point']['y']
            if not all(isinstance(v, (int, float)) and math.isfinite(v) for v in (x, y)) or not (0 < x < width and 0 < y < height):
                raise ValueError('native pointer coordinate is outside the product viewport')
            # CGWindow bounds exclude shadows and use logical screen points.
            # Only the native titlebar lies above this frameless-webview content.
            return (frame['X'] + (frame['Width'] - width) / 2 + x,
                    frame['Y'] + frame['Height'] - height + y)

        # NSWorkspace state depends on the main Cocoa runloop, which this
        # synchronous fixture does not drive. AX reads the current keyboard
        # recipient in either caller thread. Never fall back to cached state.
        # Compare once at the original failing ordinary-restart boundary, not
        # on every key. The two reads are adjacent, not an atomic focus lock.
        if request.get('compare_focus') is True:
            comparison = dict(phase='portable-restart-first-click',
                              thread='main' if threading.current_thread() is threading.main_thread() else 'reader',
                              childAlive=process.poll() is None, activationAttempted=activated)
            started = time.monotonic()
            try:
                cached = self.appkit.NSWorkspace.sharedWorkspace().frontmostApplication()
                comparison['nsAvailable'] = cached is not None
                comparison['nsMatchesTarget'] = cached.processIdentifier() == process.pid if cached is not None else None
                comparison['nsReadSeconds'] = time.monotonic() - started
                live_matches = self.focused_pid() == process.pid
                comparison['axMatchesTarget'] = live_matches
            except RuntimeError as error:
                comparison['error'] = str(error)
                raise
            finally:
                comparison['elapsedSeconds'] = time.monotonic() - started
                (self.evidence / 'OS_INPUT_FOCUS.json').write_text(json.dumps(comparison, indent=2) + '\n')
        else:
            live_matches = self.focused_pid() == process.pid
        self.counts['focus-checks'] = self.counts.get('focus-checks', 0) + 1
        if not live_matches:
            raise RuntimeError('native input target did not gain foreground focus')
        if process.poll() is not None:
            raise RuntimeError('native input target process exited during focus check')

        action = request['action']
        if action == 'click':
            if self.down:
                raise ValueError('cannot click during a held drag')
            self.ui.click(*point())
        elif action == 'double-click':
            if self.down:
                raise ValueError('cannot double-click during a held drag')
            # PyAutoGUI 0.9.54's Mac backend emits repeated single clicks:
            # its CGEvents never set the native click-state field. Retain
            # the same real OS sender and guards, changing only this action's
            # four down/up events to the documented 1,1,2,2 click states.
            quartz, original_post = self.quartz, self.quartz.CGEventPost
            expected = (quartz.kCGEventLeftMouseDown, quartz.kCGEventLeftMouseUp) * 2
            posted = 0

            def post(tap, event):
                nonlocal posted
                kind = quartz.CGEventGetType(event)
                if kind not in (quartz.kCGEventLeftMouseDown, quartz.kCGEventLeftMouseUp):
                    return original_post(tap, event)
                if posted >= 4 or kind != expected[posted]:
                    raise RuntimeError('native double-click has an unexpected event sequence')
                self.ui.failSafeCheck()
                if process.poll() is not None or self.focused_pid() != process.pid:
                    raise RuntimeError('native double-click lost its live foreground target')
                state = 1 + posted // 2
                quartz.CGEventSetIntegerValueField(event, quartz.kCGMouseEventClickState, state)
                if quartz.CGEventGetIntegerValueField(event, quartz.kCGMouseEventClickState) != state:
                    raise RuntimeError('native double-click state could not be set')
                if kind == quartz.kCGEventLeftMouseDown:
                    self.down = True
                original_post(tap, event)
                if kind == quartz.kCGEventLeftMouseUp:
                    self.down = False
                posted += 1
                self.counts['double-click-state-events'] = self.counts.get('double-click-state-events', 0) + 1

            # Actions are serialized; restore the original post function even
            # on an error. Existing close() releases only our own held button.
            quartz.CGEventPost = post
            try:
                self.ui.doubleClick(*point(), interval=0.1, button='left')
                if posted != 4:
                    raise RuntimeError('native double-click did not send exactly two complete clicks')
            finally:
                quartz.CGEventPost = original_post
        elif action == 'down':
            self.ui.moveTo(*point(), duration=0.15)
            self.ui.mouseDown(button='left'); self.down = True
        elif action == 'drag':
            if not self.down:
                raise ValueError('drag requires a held button')
            # The macOS drag backend accepts physical buttons, not dragTo's
            # default "primary" alias. Keep the same held left button through
            # all three actions; do not synthesize another down/up pair.
            self.ui.dragTo(*point(), duration=0.5, button='left', mouseDownUp=False)
        elif action == 'up':
            if not self.down:
                raise ValueError('release requires a held button')
            self.ui.mouseUp(*point(), button='left'); self.down = False
        elif action == 'scroll':
            clicks = request['clicks']
            if self.down or type(clicks) is not int or not 1 <= abs(clicks) <= 8:
                raise ValueError('native scrolling needs a small wheel step with no held button')
            self.ui.moveTo(*point(), duration=0.15)
            self.ui.scroll(clicks)
        elif action == 'keys':
            keys = request['keys']
            allowed = {'command', 'ctrl', 'shift', 'left', 'right', 'backspace', 'enter', 'tab', 'esc', 'a', 'd', 'h', 't', 'q'}
            if not isinstance(keys, list) or not 1 <= len(keys) <= 3 or not all(k in allowed for k in keys):
                raise ValueError('unsupported native acceptance key chord')
            self.ui.hotkey(*keys)
        elif action == 'text':
            text = request['text']
            if not isinstance(text, str) or not 0 < len(text) <= 2048 or any(ord(c) < 32 or ord(c) > 126 for c in text):
                raise ValueError('native acceptance typing accepts bounded printable ASCII only')
            with self.text_key_events():
                self.ui.write(text, interval=0.02)
        elif action == 'capture':
            label = request['label']
            if not isinstance(label, str) or not re.fullmatch(r'[a-z0-9-]{1,48}', label):
                raise ValueError('invalid native evidence label')
            # Call only with the workspace/dialog visible, never a private-key
            # editor. Restrict capture to the already-verified app window.
            subprocess.run(['screencapture', '-x', '-l', str(window['kCGWindowNumber']),
                            str(self.evidence / (label + '.png'))], check=True, timeout=5)
        else:
            raise ValueError('unsupported native input action')
        self.counts[action] = self.counts.get(action, 0) + 1

    def focused_pid(self):
        # Fresh synchronous AX reads in the calling thread, no AX observer,
        # cross-thread runloop pumping, field-value access or input fallback.
        status, focused = self.ax.AXUIElementCopyAttributeValue(
            self.system_focus, self.ax.kAXFocusedApplicationAttribute, None)
        if status != self.ax.kAXErrorSuccess or focused is None:
            raise RuntimeError(f'native keyboard focus unavailable (AX {status})')
        if not isinstance(focused, self.ax.AXUIElementRef):
            raise RuntimeError('native keyboard focus returned an unexpected object type')
        status, pid = self.ax.AXUIElementGetPid(focused, None)
        if status != self.ax.kAXErrorSuccess or not isinstance(pid, int) or pid <= 1:
            raise RuntimeError(f'native keyboard focus identity unavailable (AX {status})')
        return pid

    def close(self):
        if self.down:
            # Release only our own held button, including an assertion failure
            # between drag and drop. The normal fail-safe may have caused that
            # failure: cleanup must not re-enter it or require focus/activation.
            # Stay at the current pointer location; never move, press or retry
            # the interrupted action. Keep ownership if event creation/post fails.
            location = self.ui.position()
            event = self.quartz.CGEventCreateMouseEvent(None, self.quartz.kCGEventLeftMouseUp,
                                                      (location.x, location.y), self.quartz.kCGMouseButtonLeft)
            if event is None:
                raise RuntimeError('owned mouse release event could not be created')
            self.quartz.CGEventPost(self.quartz.kCGHIDEventTap, event)
            self.down = False
            self.counts['owned-release-posts'] = self.counts.get('owned-release-posts', 0) + 1
