// Hosted-only observation of the normal, no-argument product window.
// No WebView injection, field-value writes, permission prompts or TCC changes.
// Public APIs: AXUIElementCreateApplication, AXUIElementCopyAttributeValue,
// AXValueGetValue (developer.apple.com/documentation/applicationservices).
import AppKit
import ApplicationServices
import Foundation
import Darwin

enum ObservationError: Error { case failed(String), staleElement }
func require(_ condition: Bool, _ message: String) throws {
    if !condition { throw ObservationError.failed(message) }
}
func attribute(_ element: AXUIElement, _ name: String) throws -> CFTypeRef? {
    var value: CFTypeRef?
    let error = AXUIElementCopyAttributeValue(element, name as CFString, &value)
    if error == .invalidUIElement { throw ObservationError.staleElement }
    if error == .noValue || error == .attributeUnsupported || error == .cannotComplete { return nil }
    try require(error == .success, "AX attribute unavailable (\(error.rawValue))")
    return value
}
func rect(_ element: AXUIElement) throws -> CGRect {
    guard let position = try attribute(element, kAXPositionAttribute),
          let size = try attribute(element, kAXSizeAttribute) else {
        throw ObservationError.failed("AX control geometry unavailable")
    }
    try require(CFGetTypeID(position) == AXValueGetTypeID() && CFGetTypeID(size) == AXValueGetTypeID(), "invalid AX geometry types")
    var origin = CGPoint.zero, dimensions = CGSize.zero
    try require(AXValueGetValue(position as! AXValue, .cgPoint, &origin) &&
                AXValueGetValue(size as! AXValue, .cgSize, &dimensions), "AX geometry decode failed")
    return CGRect(origin: origin, size: dimensions)
}
func fit(_ window: AXUIElement) throws {
    // The same minimum-size window used by the existing native acceptance,
    // sized through the OS, never through a product test hook or DOM change.
    var position = CGPoint(x: 40, y: 40), size = CGSize(width: 1080, height: 680)
    guard let p = AXValueCreate(.cgPoint, &position), let s = AXValueCreate(.cgSize, &size) else {
        throw ObservationError.failed("AX window geometry creation failed")
    }
    try require(AXUIElementSetAttributeValue(window, kAXSizeAttribute as CFString, s) == .success &&
                AXUIElementSetAttributeValue(window, kAXPositionAttribute as CFString, p) == .success, "normal window cannot be fitted by OS accessibility")
}
func inspect(_ application: AXUIElement, hint: String, resize: inout Bool) throws -> [String: Any]? {
    guard let windows = try attribute(application, kAXWindowsAttribute) as? [AXUIElement], !windows.isEmpty else { return nil }
    try require(windows.count == 1, "normal startup needs exactly one application window")
    if resize { try fit(windows[0]); resize = false }
    var queue = [windows[0]], index = 0
    var webArea: CGRect?, fields: [CGRect] = []
    var create: CGRect?, unlock: CGRect?, lock: CGRect?, config: CGRect?
    var matchedHint = false, wrongPassword = false
    var hintSources = ["valueContains": false, "valueTrimmed": false, "titleExact": false, "descriptionExact": false]
    while index < queue.count && index < 2048 {
        let node = queue[index]; index += 1
        let role = try attribute(node, kAXRoleAttribute) as? String ?? ""
        if role == "AXWebArea" && webArea == nil { webArea = try rect(node) }
        if role == kAXTextFieldRole { fields.append(try rect(node)) }
        if role == kAXButtonRole {
            let title = try attribute(node, kAXTitleAttribute) as? String ?? ""
            let description = try attribute(node, kAXDescriptionAttribute) as? String ?? ""
            let label = title.isEmpty ? description : title
            switch label {
            case "创建加密保险库": create = try rect(node)
            case "解锁工作区": unlock = try rect(node)
            case "锁定": lock = try rect(node)
            case "连接配置": config = try rect(node)
            default: break
            }
        }
        if role == kAXStaticTextRole {
            // Never read AXValue from password fields. Static text is compared
            // only with these known fixture tokens and is never returned/logged.
            let text = try attribute(node, kAXValueAttribute) as? String ?? ""
            matchedHint = matchedHint || text == hint || text == "提示" + hint
            hintSources["valueContains"] = hintSources["valueContains"]! || text.contains(hint)
            hintSources["valueTrimmed"] = hintSources["valueTrimmed"]! || text.trimmingCharacters(in: .whitespacesAndNewlines) == hint
            let title = try attribute(node, kAXTitleAttribute) as? String ?? ""
            let description = try attribute(node, kAXDescriptionAttribute) as? String ?? ""
            hintSources["titleExact"] = hintSources["titleExact"]! || title == hint
            hintSources["descriptionExact"] = hintSources["descriptionExact"]! || description == hint
            wrongPassword = wrongPassword || text.contains("主密码错误或保险库损坏")
        }
        if let children = try attribute(node, kAXChildrenAttribute) as? [AXUIElement] {
            queue.append(contentsOf: children)
        }
    }
    guard let area = webArea else { return nil }
    let state = lock != nil && config != nil ? "workspace" : create != nil ? "create" : unlock != nil ? "unlock" : "unknown"
    if state != "workspace" { try require(index == queue.count, "unlock accessibility tree exceeds bounded observation") }
    func point(_ bounds: CGRect) -> [String: CGFloat] {
        ["x": bounds.midX - area.minX, "y": bounds.midY - area.minY]
    }
    var result: [String: Any] = ["state": state, "hintMatched": matchedHint, "wrongPassword": wrongPassword,
                               "hintSources": hintSources, "nodes": index,
                               "viewport": ["width": area.width, "height": area.height]]
    if state == "create" || state == "unlock" {
        result["fields"] = fields.sorted { $0.minY < $1.minY }.map(point)
        if let submit = create ?? unlock { result["submit"] = point(submit) }
    }
    if let button = lock { result["lock"] = point(button) }
    return result
}

do {
    let args = CommandLine.arguments
    let environment = ProcessInfo.processInfo.environment
    try require(environment["GITHUB_ACTIONS"] == "true" && environment["RUNNER_TEMP"] != nil, "hosted runner required")
    try require(args.count == 6, "expected pid, executable, state, public hint and fit flag")
    guard let pid = Int32(args[1]), pid > 1 else { throw ObservationError.failed("invalid target pid") }
    let binary = URL(fileURLWithPath: args[2]).resolvingSymlinksInPath().path
    let temporary = URL(fileURLWithPath: environment["RUNNER_TEMP"]!).resolvingSymlinksInPath().path
    try require(binary.hasPrefix(temporary + "/"), "target must be the disposable copied product")
    try require(AXIsProcessTrusted(), "BLOCKER: hosted accessibility permission unavailable; no prompt or bypass attempted")
    let desired = args[3], hint = args[4]
    try require(["create", "unlock", "workspace", "wrong-password"].contains(desired), "invalid expected state")
    var resize = args[5] == "fit"
    let application = AXUIElementCreateApplication(pid)
    AXUIElementSetMessagingTimeout(application, 2)
    var result: [String: Any]?
    var staleSnapshots = 0
    // One bounded blocking observer, 30/60-second backoff, no short polling.
    for delay in [0.0, 30.0, 60.0] {
        if delay > 0 { Thread.sleep(forTimeInterval: delay) }
        try require(kill(pid, 0) == 0, "normal product process exited")
        guard let process = NSRunningApplication(processIdentifier: pid) else { continue }
        try require(!process.isTerminated, "normal product process exited")
        guard let executable = process.executableURL else { continue }
        try require(executable.resolvingSymlinksInPath().path == binary, "target process identity changed")
        process.activate(options: [.activateIgnoringOtherApps])
        do {
            if let state = try inspect(application, hint: hint, resize: &resize) {
                let matched = desired == "wrong-password" ? state["state"] as? String == "unlock" && state["wrongPassword"] as? Bool == true : state["state"] as? String == desired
                if matched { result = state; break }
            }
        } catch ObservationError.staleElement {
            // A page transition can destroy nodes already queued for reading.
            // Discard the whole snapshot, never accept a partial tree. The
            // existing bounded schedule re-reads windows from the application.
            staleSnapshots += 1
        }
    }
    guard var result = result else { throw ObservationError.failed("normal startup UI observation timed out (stale snapshots: \(staleSnapshots))") }
    result["staleSnapshots"] = staleSnapshots
    let data = try JSONSerialization.data(withJSONObject: result, options: [.sortedKeys])
    FileHandle.standardOutput.write(data); FileHandle.standardOutput.write(Data([10]))
} catch {
    let message = String(describing: error)
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}
