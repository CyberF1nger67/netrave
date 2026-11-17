import { useEffect, useRef, useState } from 'react';
import { Icon } from './Icon';
import { setLanguage, t, useLanguageStore } from '../i18n';

// tone="app" uses theme tokens; tone="auth" uses a self-contained palette so the
// sign-in screen keeps its look even before a session theme is applied.
export function LanguageSwitcher({ tone = 'app' }: { tone?: 'app' | 'auth' }) {
  const language = useLanguageStore((state) => state.language);
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const auth = tone === 'auth';

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', close);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('mousedown', close);
      document.removeEventListener('keydown', escape);
    };
  }, [open]);

  const choose = (next: 'en' | 'ru') => { setLanguage(next); setOpen(false); };
  const itemClass = (active: boolean) => auth
    ? `block w-full! rounded-lg! border-0! px-3! py-2! text-left! text-[13px]! font-[inherit]! shadow-none! hover:bg-[#18222f]! ${active ? 'text-[#6366f1]! font-semibold!' : 'bg-transparent! text-[#e7edf5]!'}`
    : `inline-flex min-h-0 items-center gap-1.5 rounded-app-sm border-0 bg-transparent px-2.5 py-1.5 text-[12.5px] font-semibold ${active ? 'bg-accent-soft text-accent' : 'text-text hover:bg-panel2'}`;

  return (
    <div className="relative" ref={ref}>
      {auth ? (
        <button
          type="button"
          title={t('Language')}
          aria-label={t('Language')}
          aria-haspopup="menu"
          aria-expanded={open}
          onClick={(event) => { event.stopPropagation(); setOpen((value) => !value); }}
          className="inline-flex! h-10! min-h-0! items-center! gap-2! rounded-xl! border! border-[#22303f]! bg-[#121a25]! px-3! font-[inherit]! text-[13px]! font-semibold! text-[#e7edf5]! shadow-none! transition-colors! duration-200 hover:border-[#2e3e52]! hover:bg-[#18222f]! focus-visible:outline-none! focus-visible:ring-2! focus-visible:ring-[#6366f1]! motion-reduce:transition-none"
        >
          <Icon name="globe" size={18} className="inline-block shrink-0" />
          <span>{language === 'ru' ? 'Русский' : 'English'}</span>
        </button>
      ) : (
        <button type="button" className="inline-flex size-auto min-w-[38px] items-center gap-1.5 border-transparent bg-transparent px-2.5 text-muted hover:bg-panel2 hover:text-text" title={t('Language')} aria-label={t('Language')}
          aria-haspopup="menu" aria-expanded={open} onClick={(event) => { event.stopPropagation(); setOpen((value) => !value); }}>
          <Icon name="globe" size={18} />
          <span className="text-[12.5px] font-semibold max-[900px]:hidden">{language === 'ru' ? 'Русский' : 'English'}</span>
        </button>
      )}
      {open && (
        <div className={auth ? 'absolute right-0 z-20 mt-2 w-40 rounded-xl border border-[#22303f] bg-[#121a25] p-1 shadow-[0_24px_60px_-20px_rgba(0,0,0,0.8)]' : 'absolute right-0 z-20 mt-2 min-w-[150px] rounded-app-sm border border-line bg-panel p-1.5 shadow-app'} role="menu">
          <button type="button" className={itemClass(language === 'en')} role="menuitemradio" aria-checked={language === 'en'}
            onClick={() => choose('en')}>English</button>
          <button type="button" className={itemClass(language === 'ru')} role="menuitemradio" aria-checked={language === 'ru'}
            onClick={() => choose('ru')}>Русский</button>
        </div>
      )}
    </div>
  );
}
