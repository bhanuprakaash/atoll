import type { InputHTMLAttributes, TextareaHTMLAttributes } from 'react';

const base = 'text-ink outline-none placeholder:text-muted';
const sizes = {
  sm: 'h-9 rounded-xl border border-outline bg-bg px-3 text-sm focus:border-muted',
  md: 'h-12 rounded-2xl border border-tile bg-box px-4 text-base focus:border-muted',
  lg: 'h-13 rounded-[18px] border border-outline bg-box px-4.5 text-[17px] focus:border-muted',
  bare: 'min-w-0 bg-transparent text-sm',
};
const areas = {
  md: 'rounded-2xl border border-tile bg-box px-4 py-3 text-base leading-normal resize-y focus:border-muted',
  code: 'min-h-[420px] flex-1 resize-y rounded-2xl border border-line bg-well p-4 font-mono text-[13px] leading-[1.7] text-ink/80 focus:border-muted',
  bare: 'resize-none bg-transparent',
};

export function TextField({
  size = 'md',
  width = 'w-full',
  className = '',
  ...props
}: { size?: keyof typeof sizes; width?: string } & Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'size'
>) {
  return <input className={`${base} ${width} ${sizes[size]} ${className}`} {...props} />;
}

export function TextArea({
  variant = 'md',
  className = '',
  ...props
}: { variant?: keyof typeof areas } & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`${base} w-full ${areas[variant]} ${className}`} {...props} />;
}
