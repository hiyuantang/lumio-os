// SPDX-License-Identifier: AGPL-3.0-only
import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function base({ size = 18, ...rest }: IconProps, children: React.ReactNode) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...rest}
    >
      {children}
    </svg>
  );
}

export function IconHome(p: IconProps) {
  return base(
    p,
    <>
      <path d="M4 11.5 12 4l8 7.5" />
      <path d="M6 10.5V20h12v-9.5" />
      <path d="M10 20v-5h4v5" />
    </>,
  );
}

export function IconFolder(p: IconProps) {
  return base(
    p,
    <>
      <path d="M3.5 6.5h6l2 2.5h9v9.5a1.5 1.5 0 0 1-1.5 1.5H5a1.5 1.5 0 0 1-1.5-1.5v-12Z" />
    </>,
  );
}

export function IconFile(p: IconProps) {
  return base(
    p,
    <>
      <path d="M6 3.5h8L18.5 8v12a1 1 0 0 1-1 1h-11a1 1 0 0 1-1-1V4.5a1 1 0 0 1 1-1Z" />
      <path d="M13.5 3.5V8.5H19" />
    </>,
  );
}

export function IconTerminal(p: IconProps) {
  return base(
    p,
    <>
      <rect x="3" y="4.5" width="18" height="15" rx="2" />
      <path d="m7 9.5 3 2.75L7 15" />
      <path d="M12.5 15.5H17" />
    </>,
  );
}

export function IconGear(p: IconProps) {
  return base(
    p,
    <>
      <circle cx="12" cy="12" r="3.2" />
      <path d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8" />
    </>,
  );
}

export function IconList(p: IconProps) {
  return base(
    p,
    <>
      <path d="M9 6.5h11M9 12h11M9 17.5h11" />
      <circle cx="5" cy="6.5" r="0.9" fill="currentColor" stroke="none" />
      <circle cx="5" cy="12" r="0.9" fill="currentColor" stroke="none" />
      <circle cx="5" cy="17.5" r="0.9" fill="currentColor" stroke="none" />
    </>,
  );
}

export function IconBell(p: IconProps) {
  return base(
    p,
    <>
      <path d="M6 16.5v-5a6 6 0 0 1 12 0v5l1.5 2.5h-15L6 16.5Z" />
      <path d="M10 21a2.1 2.1 0 0 0 4 0" />
    </>,
  );
}

export function IconSearch(p: IconProps) {
  return base(
    p,
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m16 16 4.5 4.5" />
    </>,
  );
}

export function IconUser(p: IconProps) {
  return base(
    p,
    <>
      <circle cx="12" cy="8" r="3.6" />
      <path d="M5 20c1.2-3.4 3.9-5 7-5s5.8 1.6 7 5" />
    </>,
  );
}

export function IconChevronRight(p: IconProps) {
  return base(p, <path d="m9 5 7 7-7 7" />);
}

export function IconX(p: IconProps) {
  return base(p, <path d="m6 6 12 12M18 6 6 18" />);
}

export function IconMinus(p: IconProps) {
  return base(p, <path d="M5.5 12h13" />);
}

export function IconZoom(p: IconProps) {
  return base(
    p,
    <>
      <path d="M9 15H6.5A1.5 1.5 0 0 1 5 13.5v-7A1.5 1.5 0 0 1 6.5 5h7A1.5 1.5 0 0 1 15 6.5V9" />
      <rect x="9" y="9" width="10" height="10" rx="1.5" />
    </>,
  );
}

export function IconEye(p: IconProps) {
  return base(
    p,
    <>
      <path d="M2.5 12S6 5.8 12 5.8 21.5 12 21.5 12 18 18.2 12 18.2 2.5 12 2.5 12Z" />
      <circle cx="12" cy="12" r="2.8" />
    </>,
  );
}

export function IconPause(p: IconProps) {
  return base(p, <path d="M9 5.5v13M15 5.5v13" />);
}

export function IconPlay(p: IconProps) {
  return base(p, <path d="M8 5.5v13l10-6.5-10-6.5Z" />);
}

export function IconChip(p: IconProps) {
  return base(
    p,
    <>
      <rect x="6.5" y="6.5" width="11" height="11" rx="1.5" />
      <rect x="10" y="10" width="4" height="4" rx="0.8" />
      <path d="M9.5 3v3.5M14.5 3v3.5M9.5 17.5V21M14.5 17.5V21M3 9.5h3.5M3 14.5h3.5M17.5 9.5H21M17.5 14.5H21" />
    </>,
  );
}

export function IconNetwork(p: IconProps) {
  return base(
    p,
    <>
      <path d="M7 4v9M4.5 10.5 7 13l2.5-2.5" />
      <path d="M17 20v-9M14.5 13.5 17 11l2.5 2.5" />
    </>,
  );
}
