// Self-contained dark palette for the sign-in screen. Values mirror the app's
// dark theme, but are written literally so Tailwind can detect every class and
// the screen remains independent of shared theme custom properties.
// Trailing "!" marks utilities that must beat the global element defaults.

export const authUi = {
  root: 'min-h-dvh bg-[#0a0f16] font-[Inter,ui-sans-serif,system-ui,-apple-system,Segoe_UI,Roboto,sans-serif] text-[#e7edf5] antialiased',
  fadeIn: 'transition-all duration-700 ease-out motion-reduce:transition-none',
  fadeHidden: 'opacity-0 translate-y-3 motion-reduce:opacity-100 motion-reduce:translate-y-0',
  fadeShown: 'opacity-100 translate-y-0',
  label: 'mb-2! mt-0! block! text-xs! font-semibold! uppercase! tracking-[0.08em]! text-[#8b99ad]! m-0!',
  input: 'h-12! min-h-0! w-full! rounded-xl! border! border-[#22303f]! bg-[#0e1520]! py-0! pl-11! pr-4! text-[15px]! leading-normal! text-[#e7edf5]! shadow-none! outline-none! font-[inherit]! placeholder:text-[#8b99ad]! transition-[border-color,background-color,box-shadow] duration-200 hover:border-[#2e3e52]! focus:border-[#6366f1]! focus:bg-[#0a0f16]! focus:shadow-[0_0_0_3px_rgba(99,102,241,0.28)]! focus-visible:outline-none! motion-reduce:transition-none',
  inputWithToggle: 'pr-12!',
  inputInvalid: 'border-[#f25f5c]! focus:border-[#f25f5c]!',
  leadingIcon: 'pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[#8b99ad]',
  toggleButton: 'absolute! right-1.5! top-1/2! grid! h-11! w-11! min-h-0! -translate-y-1/2! place-items-center! rounded-lg! border-0! bg-transparent! p-0! text-[#8b99ad]! shadow-none! transition-colors! duration-200 hover:bg-[#18222f]! hover:text-[#e7edf5]! focus-visible:outline-none! focus-visible:ring-2! focus-visible:ring-[#6366f1]! motion-reduce:transition-none',
  submit: 'mt-6! flex! h-12! min-h-0! w-full! items-center! justify-center! gap-2! whitespace-nowrap! rounded-xl! border-0! bg-gradient-to-r! from-[#6366f1]! to-[#3b82f6]! px-5! py-0! font-[inherit]! text-[15px]! font-semibold! text-white! shadow-[0_18px_40px_-16px_rgba(99,102,241,0.6)]! transition-[filter,box-shadow,opacity] duration-200! hover:brightness-110! focus-visible:outline-none! focus-visible:ring-2! focus-visible:ring-[#a5b4fc]! focus-visible:ring-offset-2! focus-visible:ring-offset-[#0a0f16]! disabled:cursor-not-allowed! disabled:opacity-70! disabled:shadow-none! motion-reduce:transition-none',
  alertError: 'mt-4 flex items-start gap-2.5 rounded-xl border border-[#f25f5c]/40 bg-[#f25f5c]/12 px-3.5 py-3 text-[13px] leading-snug text-[#f25f5c]',
  alertWarn: 'mt-4 flex items-start gap-2.5 rounded-xl border border-[#f5a524]/50 bg-[#f5a524]/10 px-3.5 py-3 text-[13px] leading-snug text-[#e7edf5]',
  panelCard: 'relative overflow-hidden rounded-3xl border border-[#22303f] bg-[#121a25]/95 p-6 shadow-[0_40px_100px_-40px_rgba(0,0,0,0.7)] backdrop-blur-2xl sm:p-8',
} as const;
