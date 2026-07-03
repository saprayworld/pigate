// Timezone list helpers.
//
// The canonical list comes from the browser's own IANA database via
// Intl.supportedValuesOf("timeZone") (400+ zones, no backend endpoint needed).
// A short fallback is used only on ancient engines that lack that API. The
// GMT offset is computed at render time — the value we *store* is always the
// bare IANA name, never the offset-decorated label.

const FALLBACK_ZONES = [
  "UTC",
  "Asia/Bangkok",
  "Asia/Singapore",
  "Asia/Tokyo",
  "Asia/Kolkata",
  "Asia/Dubai",
  "Europe/London",
  "Europe/Paris",
  "America/New_York",
  "America/Los_Angeles",
  "Australia/Sydney",
];

export function getSupportedTimeZones(): string[] {
  try {
    const anyIntl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] };
    if (typeof anyIntl.supportedValuesOf === "function") {
      const zones = anyIntl.supportedValuesOf("timeZone");
      if (Array.isArray(zones) && zones.length > 0) {
        return zones;
      }
    }
  } catch {
    // fall through to fallback
  }
  return FALLBACK_ZONES;
}

// Returns e.g. "GMT+07:00" for the given IANA zone, or "" if it can't be
// determined (e.g. an unknown/legacy value).
export function getGmtOffsetLabel(timeZone: string): string {
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone,
      timeZoneName: "longOffset",
    }).formatToParts(new Date());
    const tzName = parts.find((p) => p.type === "timeZoneName")?.value ?? "";
    // longOffset yields "GMT+07:00"; UTC yields just "GMT".
    return tzName || "";
  } catch {
    return "";
  }
}

export interface TimeZoneOption {
  value: string; // bare IANA name — this is what gets stored
  label: string; // "Asia/Bangkok (GMT+07:00)"
  offsetMinutes: number; // for sorting
}

function offsetToMinutes(offsetLabel: string): number {
  // "GMT+07:00" / "GMT-05:30" / "GMT"
  const m = offsetLabel.match(/GMT([+-])(\d{2}):(\d{2})/);
  if (!m) return 0;
  const sign = m[1] === "-" ? -1 : 1;
  return sign * (parseInt(m[2], 10) * 60 + parseInt(m[3], 10));
}

// Build the option list, sorted by offset then name, with a bare-name entry for
// `selected` injected if it isn't already present (so a legacy/unknown stored
// value still shows up as the current selection rather than silently vanishing).
export function buildTimeZoneOptions(selected?: string): TimeZoneOption[] {
  const zones = getSupportedTimeZones();
  const set = new Set(zones);
  if (selected && !set.has(selected)) {
    zones.push(selected);
  }

  const options = zones.map((value) => {
    const offset = getGmtOffsetLabel(value);
    return {
      value,
      label: offset ? `${value} (${offset})` : value,
      offsetMinutes: offsetToMinutes(offset),
    };
  });

  options.sort((a, b) => a.offsetMinutes - b.offsetMinutes || a.value.localeCompare(b.value));
  return options;
}
