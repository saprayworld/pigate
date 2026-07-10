// Single source of truth for interface display labels (issue #25): alias-first
// "alias (name)", collapsed to just "name" when the alias is empty or equal to
// the OS name. Only the label is formatted here — dropdown/option values must
// stay the OS name, since firewall/dhcp/qos/route configs reference interfaces
// by their kernel name.

export interface IfaceLabelSource {
  name: string
  alias?: string
}

/** Label for an interface object you already have. */
export function ifaceLabel(iface: IfaceLabelSource): string {
  return iface.alias && iface.alias !== iface.name
    ? `${iface.alias} (${iface.name})`
    : iface.name
}

/** Label for an OS name, resolved against a list (falls back to the bare name). */
export function formatIfaceLabel(name: string, ifaces: IfaceLabelSource[]): string {
  const found = ifaces.find((i) => i.name === name)
  return found ? ifaceLabel(found) : name
}
