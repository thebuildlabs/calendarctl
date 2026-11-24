// calendarctl EventKit bridge.
//
// Protocol: one JSON request on stdin, one JSON response on stdout.
// Diagnostics go to stderr. This file is intentionally dependency-free so it
// can be compiled with `swiftc` alone. It is embedded with an Info.plist that
// declares the calendar usage descriptions required by macOS.

import Foundation
import EventKit
import Security
import Darwin
import MachO

// MARK: - Errors

struct BridgeError: Error {
    let code: String
    let message: String
    let hint: String

    init(_ code: String, _ message: String, _ hint: String = "") {
        self.code = code
        self.message = message
        self.hint = hint
    }
}

// MARK: - JSON helpers

func emit(_ object: [String: Any]) {
    if let data = try? JSONSerialization.data(withJSONObject: object, options: [.fragmentsAllowed]) {
        FileHandle.standardOutput.write(data)
        FileHandle.standardOutput.write(Data("\n".utf8))
    }
}

func emitError(_ error: BridgeError) {
    emit([
        "ok": false,
        "error": [
            "code": error.code,
            "message": error.message,
            "hint": error.hint
        ]
    ])
}

func stringParam(_ params: [String: Any], _ key: String) -> String? {
    guard let value = params[key] as? String else { return nil }
    return value.isEmpty ? nil : value
}

func boolParam(_ params: [String: Any], _ key: String) -> Bool? {
    return params[key] as? Bool
}

func intParam(_ params: [String: Any], _ key: String) -> Int? {
    if let number = params[key] as? NSNumber { return number.intValue }
    if let raw = params[key] as? String { return Int(raw) }
    return nil
}

func dictParam(_ params: [String: Any], _ key: String) -> [String: Any]? {
    return params[key] as? [String: Any]
}

func arrayParam(_ params: [String: Any], _ key: String) -> [[String: Any]]? {
    return params[key] as? [[String: Any]]
}

func dateParam(_ params: [String: Any], _ key: String) -> Date? {
    guard let raw = params[key] as? String, !raw.isEmpty else { return nil }
    return parseDate(raw)
}

let fractionalISO: ISO8601DateFormatter = {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return f
}()

let basicISO: ISO8601DateFormatter = {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime]
    return f
}()

func parseDate(_ raw: String) -> Date? {
    if let d = fractionalISO.date(from: raw) { return d }
    if let d = basicISO.date(from: raw) { return d }
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss"
    if let d = formatter.date(from: raw) { return d }
    formatter.dateFormat = "yyyy-MM-dd"
    return formatter.date(from: raw)
}

func isoString(_ date: Date) -> String {
    return basicISO.string(from: date)
}

// MARK: - Permissions

func authorizationState() -> String {
    if #available(macOS 14.0, *) {
        switch EKEventStore.authorizationStatus(for: .event) {
        case .fullAccess: return "authorized"
        case .writeOnly: return "writeOnly"
        case .notDetermined: return "notDetermined"
        case .denied: return "denied"
        case .restricted: return "restricted"
        @unknown default: return "unavailable"
        }
    } else {
        switch EKEventStore.authorizationStatus(for: .event) {
        case .fullAccess: return "authorized"
        case .writeOnly: return "writeOnly"
        case .authorized: return "authorized"
        case .notDetermined: return "notDetermined"
        case .denied: return "denied"
        case .restricted: return "restricted"
        @unknown default: return "unavailable"
        }
    }
}

func permissionMessage(_ state: String) -> String {
    switch state {
    case "authorized": return "Full calendar access is granted"
    case "writeOnly": return "Only write access is granted; reading events requires full access"
    case "notDetermined": return "Calendar access has not been requested yet"
    case "denied": return "Calendar access is denied"
    case "restricted": return "Calendar access is restricted by system policy"
    default: return "Calendar access is unavailable"
    }
}

/// Requests full access. The completion handler may be delivered on a
/// different queue, so we pump the main run loop while waiting for it.
func requestAccess() -> String {
    let store = EKEventStore()
    let semaphore = DispatchSemaphore(value: 0)
    let deadline = Date().addingTimeInterval(90)

    if #available(macOS 14.0, *) {
        store.requestFullAccessToEvents { granted, error in
            if let error = error {
                let ns = error as NSError
                FileHandle.standardError.write(Data("permission request failed: \(ns.domain) \(ns.code)\n".utf8))
            }
            if !granted {
                FileHandle.standardError.write(Data("permission request returned not granted\n".utf8))
            }
            semaphore.signal()
        }
    } else {
        store.requestAccess(to: .event) { granted, error in
            if let error = error {
                let ns = error as NSError
                FileHandle.standardError.write(Data("permission request failed: \(ns.domain) \(ns.code)\n".utf8))
            }
            if !granted {
                FileHandle.standardError.write(Data("permission request returned not granted\n".utf8))
            }
            semaphore.signal()
        }
    }

    while semaphore.wait(timeout: .now() + 0.1) == .timedOut {
        if Date() > deadline { break }
        RunLoop.main.run(mode: .common, before: Date().addingTimeInterval(0.1))
    }
    let state = authorizationState()
    if state == "notDetermined" {
        // macOS silently refuses TCC prompts from processes that are not
        // launched as a responsible app (e.g. a bare CLI child process).
        FileHandle.standardError.write(Data("permission prompt was not displayed by macOS\n".utf8))
    }
    return state
}

func requireAccess(writeOnlyAllowed: Bool = false) throws {
    let state = authorizationState()
    let allowed = state == "authorized" || (writeOnlyAllowed && state == "writeOnly")
    guard allowed else {
        throw BridgeError(
            "permission_denied",
            permissionMessage(state),
            "Run `calendarctl permissions request` from a Terminal window, or enable calendarctl EventKit Bridge in System Settings → Privacy & Security → Calendars"
        )
    }
}

// MARK: - Encoding

func calendarType(_ source: EKSource?) -> String {
    guard let source = source else { return "other" }
    switch source.sourceType {
    case .local: return "local"
    case .exchange: return "exchange"
    case .calDAV: return "caldav"
    case .mobileMe: return "icloud"
    case .subscribed: return "subscribed"
    case .birthdays: return "birthdays"
    @unknown default: return "other"
    }
}

func hexColor(_ cgColor: CGColor?) -> String? {
    guard let color = cgColor,
          let converted = color.converted(to: CGColorSpaceCreateDeviceRGB(), intent: .defaultIntent, options: nil),
          let components = converted.components,
          components.count >= 3 else { return nil }
    let r = Int(max(0, min(1, components[0])) * 255)
    let g = Int(max(0, min(1, components[1])) * 255)
    let b = Int(max(0, min(1, components[2])) * 255)
    return String(format: "#%02X%02X%02X", r, g, b)
}

func encodeCalendar(_ calendar: EKCalendar) -> [String: Any] {
    var result: [String: Any] = [
        "id": calendar.calendarIdentifier,
        "title": calendar.title,
        "writable": calendar.allowsContentModifications,
        "subscribed": calendar.isSubscribed,
        "source": calendar.source?.title ?? "",
        "type": calendarType(calendar.source)
    ]
    if let color = hexColor(calendar.cgColor) { result["color"] = color }
    return result
}

func frequencyString(_ frequency: EKRecurrenceFrequency) -> String {
    switch frequency {
    case .daily: return "daily"
    case .weekly: return "weekly"
    case .monthly: return "monthly"
    case .yearly: return "yearly"
    @unknown default: return "daily"
    }
}

func weekdayShort(_ weekday: EKWeekday) -> String {
    switch weekday {
    case .sunday: return "sun"
    case .monday: return "mon"
    case .tuesday: return "tue"
    case .wednesday: return "wed"
    case .thursday: return "thu"
    case .friday: return "fri"
    case .saturday: return "sat"
    @unknown default: return "mon"
    }
}

func weekdayFrom(_ raw: String) -> EKWeekday? {
    switch raw.lowercased() {
    case "sun", "sunday": return .sunday
    case "mon", "monday": return .monday
    case "tue", "tuesday": return .tuesday
    case "wed", "wednesday": return .wednesday
    case "thu", "thursday": return .thursday
    case "fri", "friday": return .friday
    case "sat", "saturday": return .saturday
    default: return nil
    }
}

func eventStatus(_ status: EKEventStatus) -> String {
    switch status {
    case .confirmed: return "confirmed"
    case .tentative: return "tentative"
    case .canceled: return "cancelled"
    case .none: return "none"
    @unknown default: return "none"
    }
}

func encodeEvent(_ event: EKEvent) -> [String: Any] {
    var result: [String: Any] = [
        "id": event.eventIdentifier ?? "",
        "calendarId": event.calendar?.calendarIdentifier ?? "",
        "calendar": event.calendar?.title ?? "",
        "title": event.title ?? "",
        "start": isoString(event.startDate),
        "end": isoString(event.endDate),
        "allDay": event.isAllDay,
        "status": eventStatus(event.status)
    ]
    if let notes = event.notes, !notes.isEmpty { result["description"] = notes }
    if let location = event.location, !location.isEmpty { result["location"] = location }
    if let url = event.url?.absoluteString, !url.isEmpty { result["url"] = url }
    if let tz = event.timeZone?.identifier, !tz.isEmpty { result["timeZone"] = tz }
    if let created = event.creationDate { result["createdAt"] = isoString(created) }
    if let modified = event.lastModifiedDate { result["modifiedAt"] = isoString(modified) }

    if let rule = event.recurrenceRules?.first {
        var rec: [String: Any] = [
            "frequency": frequencyString(rule.frequency),
            "interval": rule.interval
        ]
        if let end = rule.recurrenceEnd {
            if let until = end.endDate { rec["until"] = isoString(until) }
            if end.occurrenceCount > 0 { rec["count"] = end.occurrenceCount }
        }
        if let days = rule.daysOfTheWeek {
            let names = days.compactMap { weekdayShort($0.dayOfTheWeek) }
            if !names.isEmpty { rec["daysOfWeek"] = names }
        }
        result["recurrence"] = rec
    }

    var reminders: [[String: Any]] = []
    for alarm in event.alarms ?? [] {
        let minutes = Int(-alarm.relativeOffset / 60.0)
        reminders.append(["minutesBefore": minutes])
    }
    if !reminders.isEmpty { result["reminders"] = reminders }

    var attendees: [[String: Any]] = []
    for participant in event.attendees ?? [] {
        var attendee: [String: Any] = [:]
        if let name = participant.name, !name.isEmpty { attendee["name"] = name }
        let url = participant.url.absoluteString
        let email = url.hasPrefix("mailto:") ? String(url.dropFirst("mailto:".count)) : url
        if !email.isEmpty { attendee["email"] = email }
        if attendee.isEmpty { continue }
        attendees.append(attendee)
    }
    if !attendees.isEmpty { result["attendees"] = attendees }

    return result
}

// MARK: - Mutation helpers

func buildRecurrence(_ dict: [String: Any]) throws -> EKRecurrenceRule {
    let rawFrequency = (dict["frequency"] as? String) ?? "daily"
    let frequency: EKRecurrenceFrequency
    switch rawFrequency.lowercased() {
    case "daily": frequency = .daily
    case "weekly": frequency = .weekly
    case "monthly": frequency = .monthly
    case "yearly": frequency = .yearly
    default: throw BridgeError("invalid_input", "Invalid recurrence frequency: \(rawFrequency)")
    }

    let interval = max(1, intParam(dict, "interval") ?? 1)
    var days: [EKRecurrenceDayOfWeek]?
    if let rawDays = dict["daysOfWeek"] as? [String], !rawDays.isEmpty {
        days = rawDays.compactMap { weekdayFrom($0) }.map { EKRecurrenceDayOfWeek($0) }
    }

    var end: EKRecurrenceEnd?
    if let until = dateParam(dict, "until") {
        end = EKRecurrenceEnd(end: until)
    } else if let count = intParam(dict, "count"), count > 0 {
        end = EKRecurrenceEnd(occurrenceCount: count)
    }

    return EKRecurrenceRule(
        recurrenceWith: frequency,
        interval: interval,
        daysOfTheWeek: days,
        daysOfTheMonth: nil,
        monthsOfTheYear: nil,
        weeksOfTheYear: nil,
        daysOfTheYear: nil,
        setPositions: nil,
        end: end
    )
}

func buildAlarms(_ params: [String: Any]) -> [EKAlarm] {
    guard let reminders = arrayParam(params, "reminders") else { return [] }
    return reminders.compactMap { item in
        guard let minutes = intParam(item, "minutesBefore") else { return nil }
        return EKAlarm(relativeOffset: Double(-minutes) * 60.0)
    }
}

struct EventSpan {
    let name: String
    let ekSpan: EKSpan
    var isAll: Bool { name == "all" }
}

func spanFrom(_ params: [String: Any]) -> EventSpan {
    let raw = (stringParam(params, "span") ?? "this").lowercased()
    switch raw {
    case "future": return EventSpan(name: raw, ekSpan: .futureEvents)
    // EKSpan has no "all" case. "all" is implemented by iterating the series.
    case "all": return EventSpan(name: raw, ekSpan: .futureEvents)
    default: return EventSpan(name: "this", ekSpan: .thisEvent)
    }
}

/// Returns every occurrence of the series that `anchor` belongs to. EventKit
/// exposes calendarItemIdentifier for the series and eventIdentifier for one
/// occurrence, so this works when the caller supplies a detached occurrence.
func seriesOccurrences(_ store: EKEventStore, _ anchor: EKEvent) -> [EKEvent] {
    let seriesID = anchor.calendarItemIdentifier
    let calendar = Calendar.current
    let start = calendar.date(byAdding: .year, value: -100, to: anchor.startDate) ?? Date.distantPast
    let end = calendar.date(byAdding: .year, value: 100, to: anchor.startDate) ?? Date.distantFuture
    let calendars = anchor.calendar.map { [$0] }
    let predicate = store.predicateForEvents(withStart: start, end: end, calendars: calendars)
    var found: [EKEvent] = []
    store.enumerateEvents(matching: predicate) { event, _ in
        if event.calendarItemIdentifier == seriesID {
            found.append(event)
        }
    }
    return found
}

func applyEventFields(_ event: EKEvent, params: [String: Any], store: EKEventStore) throws {
    if let title = stringParam(params, "title") { event.title = title }
    if let description = params["description"] as? String { event.notes = description.isEmpty ? nil : description }
    if let location = params["location"] as? String { event.location = location.isEmpty ? nil : location }
    if let url = stringParam(params, "url") { event.url = URL(string: url) }
    if let allDay = boolParam(params, "allDay") { event.isAllDay = allDay }
    if let tz = stringParam(params, "timeZone") { event.timeZone = TimeZone(identifier: tz) }
    if let start = dateParam(params, "start") { event.startDate = start }
    if let end = dateParam(params, "end") { event.endDate = end }

    if let calendarID = stringParam(params, "calendarId") {
        guard let calendar = store.calendar(withIdentifier: calendarID) else {
            throw BridgeError("not_found", "Calendar not found")
        }
        event.calendar = calendar
    }

    if boolParam(params, "clearRecurrence") == true {
        event.recurrenceRules = nil
    } else if let rec = dictParam(params, "recurrence") {
        event.recurrenceRules = [try buildRecurrence(rec)]
    }

    if let _ = params["reminders"] {
        event.alarms = buildAlarms(params)
    }
}

func writableSource(_ store: EKEventStore) -> EKSource? {
    let writableIDs = Set(
        store.calendars(for: .event)
            .filter { $0.allowsContentModifications }
            .compactMap { $0.source?.sourceIdentifier }
    )
    let candidates = store.sources.filter { writableIDs.contains($0.sourceIdentifier) }
    if let icloud = candidates.first(where: { $0.sourceType == .mobileMe }) { return icloud }
    if let local = candidates.first(where: { $0.sourceType == .local }) { return local }
    return candidates.first
}

func throwNative(_ error: Error) -> BridgeError {
    let nsError = error as NSError
    // Keep messages generic so private event data is never surfaced.
    switch nsError.domain {
    case "EKErrorDomain":
        return BridgeError("unavailable", "EventKit operation failed (code \(nsError.code))")
    default:
        return BridgeError("internal_error", "Calendar operation failed (code \(nsError.code))")
    }
}

// MARK: - Commands

func commandCheckPermissions(_ params: [String: Any]) throws -> Any {
    let state = authorizationState()
    return ["state": state, "message": permissionMessage(state)]
}

func commandRequestPermissions(_ params: [String: Any]) throws -> Any {
    let state = requestAccess()
    if state == "notDetermined" {
        throw BridgeError(
            "permission_prompt_unavailable",
            "macOS did not display the calendar permission prompt",
            "Run `calendarctl permissions request` from a Terminal window, or grant access in System Settings → Privacy & Security → Calendars"
        )
    }
    return ["state": state, "message": permissionMessage(state)]
}

func commandListCalendars(_ params: [String: Any]) throws -> Any {
    try requireAccess()
    let store = EKEventStore()
    let calendars = store.calendars(for: .event)
        .sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
    return calendars.map(encodeCalendar)
}

func commandGetCalendar(_ params: [String: Any]) throws -> Any {
    try requireAccess()
    guard let id = stringParam(params, "id") else {
        throw BridgeError("invalid_input", "Calendar id is required")
    }
    let store = EKEventStore()
    guard let calendar = store.calendar(withIdentifier: id) else {
        throw BridgeError("not_found", "Calendar not found")
    }
    return encodeCalendar(calendar)
}

func commandCreateCalendar(_ params: [String: Any]) throws -> Any {
    try requireAccess(writeOnlyAllowed: true)
    guard let title = stringParam(params, "title") else {
        throw BridgeError("invalid_input", "Calendar title is required")
    }
    let store = EKEventStore()
    guard let source = writableSource(store) else {
        throw BridgeError("unavailable", "No writable calendar source is configured")
    }
    let calendar = EKCalendar(for: .event, eventStore: store)
    calendar.title = title
    calendar.source = source
    do {
        try store.saveCalendar(calendar, commit: true)
    } catch {
        throw throwNative(error)
    }
    return encodeCalendar(calendar)
}

func commandDeleteCalendar(_ params: [String: Any]) throws -> Any {
    try requireAccess(writeOnlyAllowed: true)
    guard let id = stringParam(params, "id") else {
        throw BridgeError("invalid_input", "Calendar id is required")
    }
    let store = EKEventStore()
    guard let calendar = store.calendar(withIdentifier: id) else {
        throw BridgeError("not_found", "Calendar not found")
    }
    do {
        try store.removeCalendar(calendar, commit: true)
    } catch {
        throw throwNative(error)
    }
    return ["deleted": true]
}

func commandListEvents(_ params: [String: Any]) throws -> Any {
    try requireAccess()
    let store = EKEventStore()
    guard let from = dateParam(params, "from"), let to = dateParam(params, "to") else {
        throw BridgeError("invalid_input", "Both from and to are required")
    }
    var calendars: [EKCalendar]? = nil
    if let calendarID = stringParam(params, "calendarId") {
        guard let calendar = store.calendar(withIdentifier: calendarID) else {
            throw BridgeError("not_found", "Calendar not found")
        }
        calendars = [calendar]
    }
    let predicate = store.predicateForEvents(withStart: from, end: to, calendars: calendars)
    var events = store.events(matching: predicate)

    if let term = stringParam(params, "search") {
        let needle = term.lowercased()
        events = events.filter { event in
            let fields = [event.title ?? "", event.notes ?? "", event.location ?? "", event.calendar?.title ?? ""]
            return fields.contains { $0.lowercased().contains(needle) }
        }
    }
    events.sort { $0.startDate < $1.startDate }
    if let limit = intParam(params, "limit"), limit > 0, events.count > limit {
        events = Array(events.prefix(limit))
    }
    return events.map(encodeEvent)
}

func commandGetEvent(_ params: [String: Any]) throws -> Any {
    try requireAccess()
    guard let id = stringParam(params, "id") else {
        throw BridgeError("invalid_input", "Event id is required")
    }
    let store = EKEventStore()
    guard let event = store.event(withIdentifier: id) else {
        throw BridgeError("not_found", "Event not found")
    }
    return encodeEvent(event)
}

func commandCreateEvent(_ params: [String: Any]) throws -> Any {
    try requireAccess(writeOnlyAllowed: true)
    let store = EKEventStore()
    guard let calendarID = stringParam(params, "calendarId"),
          let calendar = store.calendar(withIdentifier: calendarID) else {
        throw BridgeError("not_found", "Calendar not found")
    }
    guard stringParam(params, "title") != nil,
          dateParam(params, "start") != nil,
          dateParam(params, "end") != nil else {
        throw BridgeError("invalid_input", "title, start and end are required")
    }

    let event = EKEvent(eventStore: store)
    event.calendar = calendar
    do {
        try applyEventFields(event, params: params, store: store)
        try store.save(event, span: .thisEvent, commit: true)
    } catch let error as BridgeError {
        throw error
    } catch {
        throw throwNative(error)
    }
    return encodeEvent(event)
}

func commandUpdateEvent(_ params: [String: Any]) throws -> Any {
    try requireAccess(writeOnlyAllowed: true)
    guard let id = stringParam(params, "id") else {
        throw BridgeError("invalid_input", "Event id is required")
    }
    let store = EKEventStore()
    guard let event = store.event(withIdentifier: id) else {
        throw BridgeError("not_found", "Event not found")
    }
    let span = spanFrom(params)
    do {
        if span.isAll {
            // EKSpan cannot express "all occurrences"; update each occurrence
            // of the series individually and commit once.
            for occurrence in seriesOccurrences(store, event) {
                try applyEventFields(occurrence, params: params, store: store)
                try store.save(occurrence, span: .thisEvent, commit: false)
            }
        } else {
            try applyEventFields(event, params: params, store: store)
            try store.save(event, span: span.ekSpan, commit: true)
        }
    } catch let error as BridgeError {
        throw error
    } catch {
        throw throwNative(error)
    }
    return encodeEvent(event)
}

func commandDeleteEvent(_ params: [String: Any]) throws -> Any {
    try requireAccess(writeOnlyAllowed: true)
    guard let id = stringParam(params, "id") else {
        throw BridgeError("invalid_input", "Event id is required")
    }
    let store = EKEventStore()
    guard let event = store.event(withIdentifier: id) else {
        throw BridgeError("not_found", "Event not found")
    }
    let span = spanFrom(params)
    do {
        if span.isAll {
            // Delete every occurrence of the series, including detached
            // exceptions and occurrences before the one addressed by `id`.
            for occurrence in seriesOccurrences(store, event) {
                try store.remove(occurrence, span: .thisEvent, commit: false)
            }
        } else {
            try store.remove(event, span: span.ekSpan, commit: true)
        }
    } catch {
        throw throwNative(error)
    }
    return ["deleted": true]
}

// MARK: - Caller authentication

/// The TCC calendar grant belongs to this binary, not to individual operations.
/// Without a caller check, any local process could spawn it and read or mutate
/// calendars through a permission the user approved for one CLI workflow.
///
/// Requests use length-prefixed framing: 4 bytes of big-endian payload length
/// followed by exactly that many bytes of JSON. This lets us reject oversized
/// payloads before buffering them, and removes ambiguity about where a request
/// ends. Responses remain newline-delimited JSON on stdout.
private let maxRequestBytes = 1024 * 1024

func executablePath() -> String {
    var buffer = [CChar](repeating: 0, count: 1024)
    var size = UInt32(buffer.count)
    guard _NSGetExecutablePath(&buffer, &size) == 0 else { return "" }
    return String(cString: buffer)
}

private enum Trust {
    static let cliIdentifier = "com.thebuildlabs.calendarctl"
    static let bridgeIdentifier = "com.thebuildlabs.calendarctl.bridge"
    static let teamIDKey = "CalendarctlTrustedTeamID"
    static let extraCLIPathsKey = "CalendarctlTrustedCLIPaths"
}

private struct CodeIdentity {
    let valid: Bool
    let teamID: String?
    let identifier: String?
}

/// Reads the configuration embedded in this binary (or in its app bundle).
private func trustedInfo() -> [String: Any]? {
    if let info = Bundle.main.infoDictionary, !info.isEmpty { return info }
    var header = _mh_execute_header
    var size: UInt = 0
    guard let ptr = getsectiondata(&header, "__INFO", "__info_plist", &size) else { return nil }
    let data = Data(bytes: ptr, count: Int(size))
    return (try? PropertyListSerialization.propertyList(from: data, options: [], format: nil)) as? [String: Any]
}

/// A non-empty Team ID pins the trusted caller to a Developer ID signature.
/// An empty value denotes a local/ad-hoc build, where trust is anchored to the
/// installed helper path (see allowedCLIPaths) instead of a Team ID.
private func trustedTeamID() -> String? {
    guard let raw = trustedInfo()?[Trust.teamIDKey] as? String else { return nil }
    let team = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    return team.isEmpty ? nil : team
}

private func makeIdentity(_ raw: CFDictionary?) -> CodeIdentity? {
    guard let info = raw as? [String: Any] else { return nil }
    return CodeIdentity(
        valid: true,
        teamID: info[kSecCodeInfoTeamIdentifier as String] as? String,
        identifier: info[kSecCodeInfoIdentifier as String] as? String
    )
}

private func identity(of code: SecCode) -> CodeIdentity? {
    var staticCode: SecStaticCode?
    guard SecCodeCopyStaticCode(code, SecCSFlags(rawValue: 0), &staticCode) == errSecSuccess,
          let sc = staticCode else { return nil }
    return identity(of: sc)
}

private func identity(of code: SecStaticCode) -> CodeIdentity? {
    var raw: CFDictionary?
    guard SecCodeCopySigningInformation(code, SecCSFlags(rawValue: 0), &raw) == errSecSuccess else { return nil }
    return makeIdentity(raw)
}

private func staticIdentity(at path: String) -> CodeIdentity? {
    guard !path.isEmpty else { return nil }
    var code: SecStaticCode?
    guard SecStaticCodeCreateWithPath(URL(fileURLWithPath: path) as CFURL, SecCSFlags(rawValue: 0), &code) == errSecSuccess,
          let c = code,
          SecStaticCodeCheckValidityWithErrors(c, SecCSFlags(rawValue: 0), nil, nil) == errSecSuccess else { return nil }
    return identity(of: c)
}

/// Uses the kernel's view of the running process rather than a path lookup, so
/// replacing the executable on disk after launch cannot change what we check.
private func guestIdentity(pid: pid_t) -> CodeIdentity? {
    let attrs = [kSecGuestAttributePid as String: NSNumber(value: pid)] as CFDictionary
    var code: SecCode?
    guard SecCodeCopyGuestWithAttributes(nil, attrs, SecCSFlags(rawValue: 0), &code) == errSecSuccess,
          let c = code,
          SecCodeCheckValidityWithErrors(c, SecCSFlags(rawValue: 0), nil, nil) == errSecSuccess else { return nil }
    return identity(of: c)
}

private func resolved(_ path: String) -> String {
    URL(fileURLWithPath: path).resolvingSymlinksInPath().path
}

private func parentProcessPath() -> String? {
    var buffer = [CChar](repeating: 0, count: Int(PATH_MAX))
    let length = proc_pidpath(getppid(), &buffer, UInt32(buffer.count))
    guard length > 0 else { return nil }
    return String(cString: buffer)
}

/// The CLI must be the binary installed next to the helper, or a well-known
/// install location. Additional paths may be declared in the embedded plist.
private func allowedCLIPaths(helperPath: String) -> Set<String> {
    var paths = Set<String>()
    // Walk up from the helper: a bundled helper lives in
    // <bin>/EventKitBridge.app/Contents/MacOS, while the CLI is in <bin>.
    var dir = (helperPath as NSString).deletingLastPathComponent
    for _ in 0..<4 {
        paths.insert(resolved((dir as NSString).appendingPathComponent("calendarctl")))
        let parent = (dir as NSString).deletingLastPathComponent
        if parent == dir { break }
        dir = parent
    }
    for candidate in ["/usr/local/bin/calendarctl", "/opt/homebrew/bin/calendarctl"] {
        paths.insert(resolved(candidate))
    }
    if let extra = trustedInfo()?[Trust.extraCLIPathsKey] as? [String] {
        for candidate in extra { paths.insert(resolved(candidate)) }
    }
    return paths
}

private func identityMatches(_ id: CodeIdentity?, identifier: String, team: String?) -> Bool {
    guard let id = id, id.valid else { return false }
    // A pinned Team ID is the release anchor: only binaries signed with our
    // Developer ID are trusted, and ad-hoc signatures have no Team ID.
    if let team = team { return id.teamID == team }
    // Local/ad-hoc builds have no Team ID, so they are bound to a stable
    // signing identifier and to the installed path checked by the caller.
    return id.identifier == identifier
}

func helperIdentityIsTrusted() -> Bool {
    guard let helper = staticIdentity(at: executablePath()) else { return false }
    return identityMatches(helper, identifier: Trust.bridgeIdentifier, team: trustedTeamID())
}

/// Accepts commands only from the CLI that shipped with this helper. Trust is
/// anchored to a pinned code identity (Developer ID Team ID when configured)
/// and to the installed path, never to a basename the caller controls. The
/// per-invocation nonce (readHandshake) additionally proves the IPC contract.
func callerIsTrusted() -> Bool {
    let team = trustedTeamID()
    let helperPath = executablePath()
    guard let helper = staticIdentity(at: helperPath),
          identityMatches(helper, identifier: Trust.bridgeIdentifier, team: team) else { return false }

    guard let path = parentProcessPath(),
          allowedCLIPaths(helperPath: helperPath).contains(resolved(path)) else { return false }

    let parent = guestIdentity(pid: getppid()) ?? staticIdentity(at: path)
    return identityMatches(parent, identifier: Trust.cliIdentifier, team: team)
}

// MARK: - Permission request launch token

private func argument(_ name: String, in args: [String]) -> String? {
    guard let index = args.firstIndex(of: name), index + 1 < args.count else { return nil }
    return args[index + 1]
}

/// Consumes the one-time token created by `calendarctl permissions request`.
/// Launching the app with `--request-permissions` alone is not sufficient: the
/// caller must also present a 0600 token file written by the CLI.
///
/// Threat model: POSIX modes do not separate processes of the same user. This
/// token stops other users and accidental direct launches, but a malicious
/// same-user process can read it. It is deliberately NOT treated as a same-user
/// security boundary — the IPC path's kernel-level caller identity check
/// (`callerIsTrusted`) is the real boundary. True same-user isolation would
/// require an XPC Mach service with a SecCode/audit-session identity check.
private func consumePermissionToken(_ args: [String]) -> Bool {
    guard let path = argument("--token-file", in: args),
          let expected = argument("--token", in: args),
          !path.isEmpty, !expected.isEmpty else { return false }

    // Open without following symlinks, then inspect the opened file itself.
    // Checking and then reading by path (as Data(contentsOf:) does) is racy and
    // follows a symlink planted by a same-user process.
    let fd = open(path, O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
    guard fd >= 0 else { return false }
    defer { close(fd) }

    var st = stat()
    let privateUserBits = mode_t(S_IRWXG | S_IRWXO)
    guard fstat(fd, &st) == 0,
          (st.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG),
          st.st_uid == getuid(),
          (st.st_mode & privateUserBits) == 0,
          (st.st_mode & mode_t(S_IRWXU)) == mode_t(S_IRUSR | S_IWUSR) else { return false }

    // The containing directory must also be private to this user (the CLI
    // creates it with 0700).
    let dir = (path as NSString).deletingLastPathComponent
    var dirStat = stat()
    guard stat(dir, &dirStat) == 0,
          dirStat.st_uid == getuid(),
          (dirStat.st_mode & privateUserBits) == 0 else { return false }

    var buffer = [UInt8](repeating: 0, count: 256)
    let count = read(fd, &buffer, buffer.count)
    guard count > 0, count < buffer.count else { return false }
    let actual = String(decoding: buffer[0..<count], as: UTF8.self)
        .trimmingCharacters(in: .whitespacesAndNewlines)
    guard actual == expected else { return false }

    // Remove exactly the file we opened and validated.
    unlink(path)
    return true
}

/// Reads the secret delivered by the CLI on inherited file descriptor 3.
func readHandshake() -> String? {
    guard fcntl(3, F_GETFD) != -1 else { return nil }
    let data = FileHandle(fileDescriptor: 3, closeOnDealloc: false).readDataToEndOfFile()
    guard data.count > 0, data.count <= 256, let value = String(data: data, encoding: .utf8) else { return nil }
    return value.isEmpty ? nil : value
}

/// Reads exactly `count` bytes from stdin, buffering any over-read so the next
/// call sees the remainder. Returns nil at EOF before the frame is complete.
private final class FramedStdinReader {
    private var buffer = Data()
    private var eof = false

    func read(_ count: Int) -> Data? {
        while buffer.count < count, !eof {
            let chunk = FileHandle.standardInput.availableData
            if chunk.isEmpty { eof = true; break }
            buffer.append(chunk)
        }
        guard buffer.count >= count else { return nil }
        let out = Data(buffer.prefix(count))
        buffer.removeFirst(count)
        return out
    }
}

/// Returns the JSON body of a single length-prefixed request, or nil when the
/// frame is malformed, empty, or larger than `maxRequestBytes`.
func readRequest() -> Data? {
    let reader = FramedStdinReader()
    guard let header = reader.read(4) else { return nil }
    let length = header.reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
    guard length > 0, length <= UInt32(maxRequestBytes) else { return nil }
    return reader.read(Int(length))
}

// MARK: - Entry point

func dispatch(_ command: String, _ params: [String: Any]) throws -> Any {
    switch command {
    case "ping": return ["pong": true]
    case "checkPermissions": return try commandCheckPermissions(params)
    case "requestPermissions": return try commandRequestPermissions(params)
    case "listCalendars": return try commandListCalendars(params)
    case "getCalendar": return try commandGetCalendar(params)
    case "createCalendar": return try commandCreateCalendar(params)
    case "deleteCalendar": return try commandDeleteCalendar(params)
    case "listEvents": return try commandListEvents(params)
    case "getEvent": return try commandGetEvent(params)
    case "createEvent": return try commandCreateEvent(params)
    case "updateEvent": return try commandUpdateEvent(params)
    case "deleteEvent": return try commandDeleteEvent(params)
    default: throw BridgeError("invalid_input", "Unknown command: \(command)")
    }
}

@main
struct EventKitBridge {
    static func main() {
        // Launched by LaunchServices (e.g. `open EventKitBridge.app --args
        // --request-permissions --token-file ... --token ...`) macOS treats
        // this process as a responsible app and can display the TCC dialog.
        // The one-time token proves the launch was initiated by the CLI.
        let args = CommandLine.arguments
        if args.contains("--request-permissions") {
            guard helperIdentityIsTrusted(), consumePermissionToken(args) else {
                emitError(BridgeError(
                    "unauthorized", "Permission requests must be initiated by calendarctl",
                    "Run `calendarctl permissions request` from a Terminal window"
                ))
                return
            }
            let state = requestAccess()
            if state == "notDetermined" {
                emitError(BridgeError(
                    "permission_prompt_unavailable",
                    "macOS did not display the calendar permission prompt",
                    "Grant access in System Settings → Privacy & Security → Calendars"
                ))
            } else {
                emit(["ok": true, "data": ["state": state, "message": permissionMessage(state)]])
            }
            return
        }

        guard let input = readRequest() else {
            emitError(BridgeError("invalid_input", "Invalid or oversized request frame"))
            return
        }
        do {
            guard let request = try JSONSerialization.jsonObject(with: input) as? [String: Any] else {
                throw BridgeError("invalid_input", "Request must be a JSON object")
            }
            guard callerIsTrusted() else {
                throw BridgeError("unauthorized", "Untrusted caller",
                                  "Commands must be sent by the calendarctl CLI")
            }
            guard let expected = readHandshake(),
                  let nonce = request["nonce"] as? String,
                  nonce == expected else {
                throw BridgeError("unauthorized", "Bridge handshake failed",
                                  "Run the command through calendarctl")
            }
            let command = request["command"] as? String ?? ""
            let params = request["params"] as? [String: Any] ?? [:]
            let result = try dispatch(command, params)
            emit(["ok": true, "data": result])
        } catch let error as BridgeError {
            emitError(error)
        } catch {
            emitError(BridgeError("internal_error", "Invalid request payload"))
        }
    }
}
