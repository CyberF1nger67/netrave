import { useState } from 'react';
import type { ThemeColors } from '../../types/api';
import { applyTheme, baseOf, listThemes, readStandardColors, THEME_GROUPS, themeFromId, useThemeStore } from '../../stores/theme';
import type { ResolvedTheme } from '../../stores/theme';
import { toast } from '../../stores/toast';
import { can, useAuthStore } from '../../stores/auth';
import { Modal } from '../../components/ui/Modal';
import { t, useLanguageStore } from '../../i18n';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const card = 'relative flex h-full min-w-0 flex-col gap-2.5 rounded-app border border-line bg-panel p-3.5 shadow-app transition-[transform,border-color,box-shadow] hover:-translate-y-0.5 hover:border-line-2';

interface EditState {
  id: string | null;
  name: string;
  colors: ThemeColors;
  origin: ThemeColors;
}

export function ThemesPage() {
  const user = useAuthStore((store) => store.user);
  const canCreate = can(user, 'themes.create');
  const state = useThemeStore((store) => store.state);
  const pick = useThemeStore((store) => store.pick);
  const saveCustom = useThemeStore((store) => store.saveCustom);
  const deleteCustom = useThemeStore((store) => store.deleteCustom);
  const [edit, setEdit] = useState<EditState | null>(null);
  const [notice, setNotice] = useState('');
  useLanguageStore((state) => state.language);
  const themes = listThemes(state);

  const startEdit = (theme: ResolvedTheme, mode: 'new' | 'copy' | 'edit') => {
    const colors = theme.colors ? { ...theme.colors } : readStandardColors(theme.base);
    const name = mode === 'edit' ? theme.name : mode === 'copy' ? `${theme.name} (${t('Copy')})`.slice(0, 40) : t('My theme');
    const next = { id: mode === 'edit' ? theme.cid ?? null : null, name, colors, origin: { ...colors } };
    setEdit(next);
    setNotice('');
    applyTheme(baseOf(colors), colors);
  };

  const setColor = (key: string, value: string) => {
    if (!edit) return;
    const colors = { ...edit.colors, [key]: value };
    setEdit({ ...edit, colors });
    applyTheme(baseOf(colors), colors);
  };

  const cancelEdit = () => {
    setEdit(null);
    const current = themeFromId(state.active, state);
    applyTheme(current.base, current.colors);
  };

  const save = async () => {
    if (!edit) return;
    if (!edit.name.trim()) { toast.error(t('Theme name')); return; }
    const error = await saveCustom(edit.id, edit.name.trim(), edit.colors);
    if (error) { setNotice(`${t('Could not save theme:')} ${error}`); return; }
    setEdit(null);
    setNotice(t('Theme saved.'));
  };

  return (
    <>
      <h2>{t('Theme settings')}</h2>
      <div className="-mt-1 mb-4 text-sm text-muted">{t('Choose a ready theme or create your own. Colors apply immediately and are saved for your account.')}</div>
      {notice && <div className="mb-3 text-muted">{notice}</div>}
      {canCreate && (
        <div className="mb-3.5">
          <button type="button" className={primary} onClick={() => startEdit(themeFromId(state.active, state), 'new')}>{t('+ Create theme')}</button>
        </div>
      )}
      <div className="grid auto-rows-fr grid-cols-[repeat(auto-fill,minmax(210px,1fr))] gap-3.5">
        {themes.map((theme) => {
          const active = theme.id === state.active;
          const sw = theme.colors
            ? [theme.colors['bg'], theme.colors['panel'], theme.colors['accent'], theme.colors['accent-2'], theme.colors['text']]
            : theme.sw ?? (theme.base === 'light' ? ['#f3f6fb', '#ffffff', '#4f46e5', '#2563eb', '#0f1a2b'] : ['#0a0f16', '#121a25', '#6366f1', '#3b82f6', '#e7edf5']);
          return (
            <div key={theme.id} className={`${card} ${active ? "border-accent ring-[3px] ring-accent-soft after:absolute after:right-3.5 after:top-3.5 after:grid after:size-5 after:place-items-center after:rounded-full after:bg-accent after:text-xs after:font-bold after:leading-none after:text-on-accent after:content-['✓']" : ''}`} role="button" tabIndex={0}
              title={t('Apply theme')} onClick={() => void pick(theme.id).then((error) => { if (error) setNotice(`${t('Could not save theme:')} ${error}`); })}
              onKeyDown={(event) => { if (event.key === 'Enter') void pick(theme.id); }}>
              <div className="flex h-14 shrink-0 overflow-hidden rounded-app-sm border border-line">{sw.map((color, index) => <i key={index} className="block flex-1" style={{ background: color ?? '#000' }} />)}</div>
              <div className="min-w-0 pr-7">
                <div className="truncate font-semibold" title={theme.name}>{theme.name}</div>
                <div className="truncate text-xs leading-[18px] text-muted">{theme.kind === 'custom' ? t('My theme') : theme.kind === 'preset' ? t('Ready-made theme') : t('Standard')} · {theme.base === 'dark' ? t('Dark') : t('Light')}</div>
              </div>
              <div className="mt-auto flex min-h-7 flex-wrap items-center gap-1.5 pt-0.5 [&>button]:min-h-7 [&>button]:px-2.5 [&>button]:py-1 [&>button]:text-xs">
                {canCreate && <button type="button" onClick={(event) => { event.stopPropagation(); startEdit(theme, 'copy'); }}>{t('Copy')}</button>}
                {theme.kind === 'custom' && <button type="button" onClick={(event) => { event.stopPropagation(); startEdit(theme, 'edit'); }}>{t('Edit')}</button>}
                {theme.kind === 'custom' && <button type="button" className={danger} onClick={(event) => {
                  event.stopPropagation();
                  if (!confirm(`${t('Delete')} «${theme.name}»?`)) return;
                  if (edit?.id === theme.cid) setEdit(null);
                  void deleteCustom(theme.cid ?? '').then((error) => { if (error) setNotice(`${t('Could not save theme:')} ${error}`); });
                }}>{t('Delete')}</button>}
              </div>
            </div>
          );
        })}
      </div>
      {edit && (
        <Modal title={edit.id ? t('Edit theme') : t('New theme')} onClose={cancelEdit} wide actions={[
          { label: t('Cancel'), onClick: cancelEdit },
          { label: t('Restore originals'), onClick: () => { const colors = { ...edit.origin }; setEdit({ ...edit, colors }); applyTheme(baseOf(colors), colors); }, closeOnClick: false },
          { label: t('Save theme'), primary: true, onClick: () => save() },
        ]}>
          <div id="theme-editor" className="mb-3.5 flex flex-wrap items-center gap-2.5 [&>input]:max-w-[340px] [&>label]:my-0"><label htmlFor="theme-name">{t('Theme name')}</label><input id="theme-name" value={edit.name} maxLength={40} onChange={(event) => setEdit({ ...edit, name: event.target.value })} /></div>
          {THEME_GROUPS.map((group) => (
            <div className="[&+&]:mt-[18px]" key={group.title}>
              <h4 className="mb-2 mt-0 text-xs font-semibold uppercase tracking-[.06em] text-muted">{t(group.title as Parameters<typeof t>[0])}</h4>
              <div className="grid grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-x-[18px] gap-y-2 max-sm:grid-cols-1">
                {group.keys.map(([key, label]) => (
                  <div className="flex min-w-0 items-center gap-2.5" key={key}>
                    <span className="min-w-0 flex-1 text-[13px]">{t(label as Parameters<typeof t>[0])}</span>
                    <input className="size-11 min-h-0 flex-none cursor-pointer p-0.5" type="color" value={edit.colors[key] ?? '#000000'} aria-label={label} onChange={(event) => setColor(key, event.target.value)} />
                    <input className="w-24 flex-none font-mono text-[13px] lowercase" value={edit.colors[key] ?? ''} maxLength={7} spellCheck={false} aria-label={`${label} (HEX)`} onChange={(event) => {
                      const value = event.target.value.trim();
                      if (/^#?[0-9a-f]{6}$/i.test(value)) setColor(key, '#' + value.replace('#', '').toLowerCase());
                    }} />
                  </div>
                ))}
              </div>
            </div>
          ))}
          <div className="mt-[18px] text-xs leading-relaxed text-muted">{t('Changes are visible across the whole interface immediately.')}</div>
        </Modal>
      )}
    </>
  );
}
