/**
 * PiGate brand mark — a solid-emerald shield (firewall) with the letter π
 * (Raspberry Pi) whose footed legs form a gateway. Self-contained inline SVG so
 * it stays crisp at any size; the emerald + white knockout read correctly on
 * both light and dark grounds, so no theme handling is needed here.
 */
export function PiGateLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 64 64"
      fill="none"
      className={className}
      role="img"
      aria-label="PiGate"
    >
      <path
        d="M32 4 L53.4 11.1 A2 2 0 0 1 54.8 13 V33 C54.8 46.6 45.4 55.8 32.8 59.9 A2.5 2.5 0 0 1 31.2 59.9 C18.6 55.8 9.2 46.6 9.2 33 V13 A2 2 0 0 1 10.6 11.1 Z"
        fill="#10b981"
      />
      <g fill="#ffffff">
        <rect x="18" y="21.5" width="28" height="5.2" rx="2.6" />
        <rect x="22.4" y="24.5" width="5" height="19" rx="2.5" />
        <rect x="18.8" y="41" width="8.6" height="4.4" rx="2.2" />
        <rect x="36.6" y="24.5" width="5" height="19" rx="2.5" />
        <rect x="36.6" y="41" width="8.6" height="4.4" rx="2.2" />
      </g>
    </svg>
  )
}
