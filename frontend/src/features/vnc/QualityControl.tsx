import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { consoleApi } from '../../api/endpoints';
import { onLanguageChange, t } from '../../i18n';
import type { VncEncoding, VncQualitySettings, VncResolution } from '../../types/api';
import type { VNCView } from './VNCView';
import { DEFAULT_QUALITY, VNC_BITRATE_PRESETS, VNC_ENCODINGS, VNC_RESOLUTIONS, qualityKey } from './quality';

const TRIGGER = 'inline-flex cursor-pointer items-center gap-1 rounded-[6px] border border-line bg-transparent px-[9px] py-[3px] text-xs text-text transition-colors hover:bg-panel2';
const TRIGGER_ON = 'border-accent bg-accent-soft text-accent';
const POPOVER = 'fixed z-[95] flex flex-col gap-2.5 overflow-y-auto overscroll-contain rounded-[12px] border border-line bg-panel p-3 text-xs text-text shadow-[0_18px_44px_-18px_rgba(0,0,0,.75),0_2px_8px_rgba(0,0,0,.35)]';
const LABEL = 'text-muted';
const FIELD = 'min-w-0 flex-1 rounded-[6px] border border-line bg-bg-2 px-1.5 py-1 pr-7 text-xs text-text';
const PILL = 'cursor-pointer rounded-[6px] border border-line bg-transparent px-1.5 py-0.5 text-[11px] tabular-nums text-muted transition-colors hover:bg-panel2 hover:text-text';
const PILL_ON = 'border-accent bg-accent-soft text-accent';
const NUM = 'w-14 rounded-[6px] border border-line bg-bg-2 px-1.5 py-1 text-right text-xs tabular-nums text-text';
const RANGE = 'min-w-0 flex-1 cursor-pointer';
const BTN = 'cursor-pointer rounded-[7px] border border-line bg-panel2 px-2.5 py-1 text-xs text-text transition-colors hover:border-accent hover:text-accent disabled:cursor-default disabled:opacity-45';
const BTN_PRIMARY = 'cursor-pointer rounded-[7px] border border-transparent bg-gradient-to-br from-accent to-accent-2 px-2.5 py-1 text-xs font-semibold text-on-accent transition-[filter] hover:brightness-110 disabled:cursor-default disabled:opacity-45';

function useLanguageVersion(): void {
  const [, setVersion] = useState(0);
  useEffect(() => onLanguageChange(() => setVersion((value) => value + 1)), []);
}

interface LiveState {
  resolution: string;
  error: string;
  notice: string;
  pending: boolean;
}

function readLive(view: VNCView): LiveState {
  return {
    resolution: view.resolution,
    error: view.resolutionError,
    notice: view.resolutionNotice,
    pending: view.resolutionPending,
  };
}

// Resolution and errors come from the RFB client asynchronously: subscribing to
// VNCView's single callback updates only this panel.
function useLiveState(view: VNCView): LiveState {
  const [state, setState] = useState<LiveState>(() => readLive(view));
  useEffect(() => {
    const update = () => setState(readLive(view));
    view.onQualityChange = update;
    update();
    return () => {
      if (view.onQualityChange === update) view.onQualityChange = null;
    };
  }, [view]);
  return state;
}

interface NumberInputProps {
  label: string;
  min: number;
  max: number;
  value: number | null;
  onChange: (value: number | null) => void;
  disabled?: boolean;
}

// The field accepts multi-digit numbers character by character; an empty value means "Auto".
// Invalid numbers are not sent and revert to the last valid value on blur.
function NumberInput({ label, min, max, value, onChange, disabled }: NumberInputProps) {
  const [text, setText] = useState(value === null ? '' : String(value));
  const editing = useRef(false);
  useEffect(() => {
    if (!editing.current) setText(value === null ? '' : String(value));
  }, [value]);
  const commit = (raw: string) => {
    const parsed = Number.parseInt(raw, 10);
    if (!Number.isFinite(parsed)) return;
    const clamped = Math.max(min, Math.min(max, parsed));
    setText(String(clamped));
    onChange(clamped);
  };
  return (
    <input
      className={NUM}
      type="number"
      inputMode="numeric"
      min={min}
      max={max}
      step={1}
      value={text}
      disabled={disabled}
      aria-label={label}
      placeholder={t('Auto')}
      onFocus={() => { editing.current = true; }}
      onBlur={() => {
        editing.current = false;
        if (text === '') onChange(null);
        else commit(text);
      }}
      onChange={(event) => {
        const raw = event.target.value;
        setText(raw);
        const parsed = Number.parseInt(raw, 10);
        if (raw !== '' && Number.isFinite(parsed) && parsed >= min && parsed <= max) onChange(parsed);
      }}
    />
  );
}

interface AutoToggleProps { on: boolean; onClick: () => void }
function AutoToggle({ on, onClick }: AutoToggleProps) {
  return (
    <button type="button" className={`${PILL}${on ? ` ${PILL_ON}` : ''}`} aria-pressed={on} onClick={onClick}>
      {t('Auto')}
    </button>
  );
}

interface RangeFieldProps {
  label: string;
  hint: string;
  min: number;
  max: number;
  value: number | null;
  fallback: number;
  onChange: (value: number | null) => void;
}

function RangeField({ label, hint, min, max, value, fallback, onChange }: RangeFieldProps) {
  return (
    <div>
      <div className="flex items-center gap-1.5">
        <span className={LABEL}>{label}</span>
        <AutoToggle on={value === null} onClick={() => onChange(value === null ? fallback : null)} />
        <span className="ml-auto tabular-nums text-muted">{value === null ? t('Auto') : hint}</span>
      </div>
      <div className="mt-1 flex items-center gap-2">
        <input
          className={RANGE}
          type="range"
          min={min}
          max={max}
          step={1}
          value={value ?? fallback}
          aria-label={label}
          onChange={(event) => onChange(Number(event.target.value))}
        />
        <NumberInput label={label} min={min} max={max} value={value} onChange={onChange} />
      </div>
    </div>
  );
}

interface QualityControlProps {
  rid: number;
  view: VNCView;
}

export function QualityControl({ rid, view }: QualityControlProps) {
  useLanguageVersion();
  const live = useLiveState(view);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');
  const [applied, setApplied] = useState<VncQualitySettings | null>(null);
  // Machine template values: restored by the "Default" button.
  const [templateDefaults, setTemplateDefaults] = useState<VncQualitySettings>(DEFAULT_QUALITY);
  const [draft, setDraft] = useState<VncQualitySettings>({ ...DEFAULT_QUALITY });
  const [advanced, setAdvanced] = useState(false);
  const [place, setPlace] = useState<{ top: number; left: number; width: number; maxHeight: number } | null>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const buttonRef = useRef<HTMLButtonElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    consoleApi.quality(rid)
      .then((response) => {
        if (cancelled) return;
        setApplied(response.settings);
        setDraft(response.settings);
        setTemplateDefaults(response.defaults ?? DEFAULT_QUALITY);
        // Settings are applied to the console immediately, but without forced resolution
        // fitting: restoring must not change the frame on connect.
        // Values matching the current ones are not sent again.
        if (qualityKey(response.settings) !== qualityKey(view.quality)) {
          view.applyQuality(response.settings, { fitAuto: false });
        }
      })
      .catch((reason: unknown) => {
        if (cancelled) return;
        // Without loaded settings the form must still work:
        // the client's current values are used as the base.
        setApplied(view.quality);
        setDraft(view.quality);
        setError(reason instanceof ApiError ? reason.message : t('Could not load the image settings'));
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [rid, view]);

  const dirty = applied !== null && qualityKey(applied) !== qualityKey(draft);

  const close = useCallback(() => setOpen(false), []);

  const measure = useCallback(() => {
    const button = buttonRef.current;
    if (!button) return;
    const rect = button.getBoundingClientRect();
    const width = Math.min(340, window.innerWidth - 16);
    const below = window.innerHeight - rect.bottom - 10;
    const above = rect.top - 10;
    const openUp = below < 420 && above > below;
    const available = openUp ? above : below;
    const maxHeight = Math.max(200, Math.min(available, 560));
    const top = openUp ? Math.max(8, rect.top - maxHeight - 6) : rect.bottom + 6;
    const left = Math.min(Math.max(8, rect.left), Math.max(8, window.innerWidth - width - 8));
    setPlace({ top, left, width, maxHeight });
  }, []);

  useLayoutEffect(() => {
    if (!open) { setPlace(null); return; }
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [open, measure]);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (rootRef.current?.contains(target)) return;
      if (panelRef.current?.contains(target)) return;
      close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      // Intercept Escape before the canvas handler: otherwise it would go to the guest
      // and the popover would remain open.
      event.preventDefault();
      event.stopPropagation();
      close();
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey, true);
    };
  }, [open, close]);

  const update = (patch: Partial<VncQualitySettings>) => {
    setDraft((current) => ({ ...current, ...patch }));
    setSaved(false);
    setError('');
  };

  const apply = () => {
    if (saving) return;
    setSaving(true);
    setError('');
    consoleApi.putQuality(rid, draft)
      .then((response) => {
        setApplied(response.settings);
        setDraft(response.settings);
        // Explicitly applying "Auto" fits the guest to the console window.
        view.applyQuality(response.settings, { fitAuto: true });
        setSaved(true);
      })
      .catch((reason: unknown) => {
        setError(reason instanceof ApiError ? reason.message : t('Could not save the image settings'));
      })
      .finally(() => setSaving(false));
  };

  const reset = () => {
    if (applied) setDraft(applied);
    setError('');
    setSaved(false);
  };

  const defaults = () => {
    setDraft({ ...templateDefaults });
    setError('');
    setSaved(false);
  };

  const bitrate = draft.bitrate;
  const status = error
    ? <span className="text-bad">{error}</span>
    : live.error
      ? <span className="text-bad">{live.error}</span>
      : saving
        ? <span className={LABEL}>{t('Saving…')}</span>
        : loading
          ? <span className={LABEL}>{t('Loading…')}</span>
          : dirty
            ? <span className={LABEL}>{t('Pending changes — press Apply')}</span>
            : live.pending
              ? <span className={LABEL}>{t('The resolution is being applied…')}</span>
              : live.notice
                ? <span className={LABEL}>{live.notice}</span>
                : saved
                  ? <span className="text-ok">{t('Applied')}</span>
                  : null;

  return (
    <div ref={rootRef} className="inline-flex">
      <button
        ref={buttonRef}
        type="button"
        className={`${TRIGGER}${open ? ` ${TRIGGER_ON}` : ''}`}
        title={t('Image quality')}
        aria-label={t('Image quality')}
        aria-haspopup="dialog"
        aria-expanded={open}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => { setOpen((value) => !value); setSaved(false); }}
      >
        <span aria-hidden>⚙</span>
        <span className="max-[720px]:hidden">{t('Image quality')}</span>
        {bitrate !== null && <span className="tabular-nums text-muted">{bitrate}</span>}
        {live.error && <span className="text-bad" aria-hidden>⚠</span>}
      </button>
      {open && place && (
        <div
          ref={panelRef}
          role="dialog"
          aria-label={t('VNC image quality')}
          className={POPOVER}
          style={{ top: place.top, left: place.left, width: place.width, maxHeight: place.maxHeight }}
        >
          <div className="flex items-center gap-2">
            <span className="font-semibold">{t('Image quality')}</span>
            <span className="ml-auto tabular-nums text-muted">{t('Current resolution')}: {live.resolution}</span>
            <button type="button" className={`${PILL} ml-1`} title={t('Close')} onClick={close} aria-label={t('Close')}>✕</button>
          </div>

          <label className="flex items-center gap-2">
            <span className={LABEL}>{t('Resolution')}</span>
            <select
              className={FIELD}
              value={draft.resolution}
              onChange={(event) => update({ resolution: event.target.value as VncResolution })}
            >
              {VNC_RESOLUTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.value === 'auto' ? t('Auto (fits the window)') : option.label}
                </option>
              ))}
            </select>
          </label>

          <div>
            <div className="flex items-center gap-1.5">
              <span className={LABEL}>{t('Bitrate')}</span>
              <span className="text-muted">{t('Mbit/s')}</span>
              <AutoToggle on={bitrate === null} onClick={() => update({ bitrate: bitrate === null ? 10 : null })} />
              <NumberInput
                label={t('Bitrate')}
                min={1}
                max={100}
                value={bitrate}
                onChange={(value) => update({ bitrate: value })}
              />
            </div>
            <input
              className={`${RANGE} mt-1 w-full`}
              type="range"
              min={1}
              max={100}
              step={1}
              value={bitrate ?? 10}
              aria-label={t('Bitrate')}
              onChange={(event) => update({ bitrate: Number(event.target.value) })}
            />
            <div className="mt-1 flex flex-wrap gap-1">
              {VNC_BITRATE_PRESETS.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  className={`${PILL}${bitrate === preset.value ? ` ${PILL_ON}` : ''}`}
                  title={`${t(preset.label)} · ${preset.value} ${t('Mbit/s')}`}
                  onClick={() => update({ bitrate: preset.value })}
                >
                  {preset.value}
                </button>
              ))}
            </div>
          </div>

          <details open={advanced} onToggle={(event) => setAdvanced((event.currentTarget as HTMLDetailsElement).open)}>
            <summary className="cursor-pointer select-none text-muted">{t('Advanced')}</summary>
            <div className="mt-2 flex flex-col gap-2.5">
              <label className="flex items-center gap-2">
                <span className={LABEL}>{t('Encoding')}</span>
                <select
                  className={FIELD}
                  value={draft.encoding}
                  onChange={(event) => update({ encoding: event.target.value as VncEncoding })}
                >
                  {VNC_ENCODINGS.map((option) => (
                    <option key={option.value} value={option.value}>{t(option.label)}</option>
                  ))}
                </select>
              </label>
              <RangeField
                label={t('JPEG quality')}
                hint={draft.quality === null ? t('Auto') : `${draft.quality} / 9`}
                min={1}
                max={9}
                value={draft.quality}
                fallback={6}
                onChange={(value) => update({ quality: value })}
              />
              <RangeField
                label={t('Compression')}
                hint={draft.compression === null ? t('Auto') : `${draft.compression} / 9`}
                min={1}
                max={9}
                value={draft.compression}
                fallback={6}
                onChange={(value) => update({ compression: value })}
              />
              <RangeField
                label={t('Frame rate')}
                hint={draft.fps === null ? t('Auto') : `${draft.fps} ${t('FPS')}`}
                min={1}
                max={60}
                value={draft.fps}
                fallback={30}
                onChange={(value) => update({ fps: value })}
              />
            </div>
          </details>

          <div className="min-h-4">{status}</div>

          <div className="flex flex-wrap items-center gap-1.5">
            <button type="button" className={BTN_PRIMARY} disabled={saving || loading || !dirty} onClick={apply}>
              {saving ? t('Saving…') : t('Apply')}
            </button>
            <button type="button" className={BTN} disabled={saving || !dirty} onClick={reset}>{t('Reset')}</button>
            <button type="button" className={BTN} disabled={saving} onClick={defaults}>{t('Default')}</button>
            <span className="ml-auto text-[11px] text-muted">{t('1–100 Mbit/s is a target, not a network cap')}</span>
          </div>
        </div>
      )}
    </div>
  );
}
