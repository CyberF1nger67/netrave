import { create } from 'zustand';
import { authApi } from '../api/endpoints';
import type { CustomTheme, ThemeCatalog, ThemeColors, ThemePreset, ThemeState } from '../types/api';
import { t } from '../i18n';
import { withAlpha } from '../lib/color';

export const THEME_GROUPS: Array<{ title: string; keys: Array<[string, string]> }> = [
  { title: 'Background and panels', keys: [['bg', 'Page background'], ['bg-2', 'Inputs'], ['panel', 'Panels and cards'], ['panel2', 'Hover backgrounds'], ['panel3', 'Accent backgrounds'], ['side', 'Sidebar']] },
  { title: 'Buttons and borders', keys: [['btn', 'Buttons'], ['line', 'Borders'], ['line-2', 'Hover borders']] },
  { title: 'Text', keys: [['text', 'Primary text'], ['muted', 'Muted text']] },
  { title: 'Accents', keys: [['accent', 'Accent'], ['accent-2', 'Accent (gradient)'], ['on-accent', 'Text on accent buttons']] },
  { title: 'Statuses', keys: [['ok', 'Success'], ['bad', 'Error'], ['warn', 'Warning']] },
];

const THEME_KEYS = THEME_GROUPS.flatMap((group) => group.keys.map(([key]) => key));
const THEME_VARS = [...THEME_KEYS, 'accent-soft'];
const MAX_CUSTOM = 30;

/**
  * Themes are loaded from the server from YAML files in the shared catalog.
  * Before loading, the catalog is empty: the default dark palette from CSS is applied.
 */
let themeCatalog: ThemeCatalog = { standard: [], presets: [] };

function setThemeCatalog(catalog: ThemeCatalog): void {
  themeCatalog = {
    standard: Array.isArray(catalog.standard) ? catalog.standard : [],
    presets: Array.isArray(catalog.presets) ? catalog.presets : [],
  };
}

export interface ResolvedTheme {
  id: string;
  name: string;
  base: 'dark' | 'light';
  colors: ThemeColors | null;
  kind: 'standard' | 'preset' | 'custom';
  cid?: string;
  sw?: string[] | undefined;
}

function standardTheme(id: string): ThemePreset | undefined {
  return themeCatalog.standard.find((item) => item.id === id);
}

function presetTheme(id: string): ThemePreset | undefined {
  return themeCatalog.presets.find((item) => item.id === id);
}

export function themeFromId(id: string, state: ThemeState): ResolvedTheme {
  const standard = standardTheme(id);
  if (standard) {
    return { id, name: standard.name, base: standard.base, colors: standard.colors ?? null, kind: 'standard', sw: standard.sw };
  }
  if (id.startsWith('preset:')) {
    const presetId = id.slice(7);
    const preset = presetTheme(presetId);
    const override = state.presets?.[presetId];
    if (preset && !state.hidden?.includes(presetId)) {
      if (override) return { id, name: override.name, base: override.base, colors: override.colors, kind: 'preset', sw: preset.sw };
      if (preset.colors) return { id, name: preset.name, base: preset.base, colors: preset.colors, kind: 'preset', sw: preset.sw };
    }
  }
  if (id.startsWith('custom:')) {
    const custom = state.custom.find((item) => `custom:${item.id}` === id);
    if (custom) return { id, name: custom.name, base: custom.base, colors: custom.colors, kind: 'custom', cid: custom.id };
  }
  const fallback = standardTheme('dark');
  return { id: 'dark', name: fallback?.name ?? 'Dark', base: fallback?.base ?? 'dark', colors: null, kind: 'standard', sw: fallback?.sw };
}

export function applyTheme(base: 'dark' | 'light', colors: ThemeColors | null): void {
  const root = document.documentElement;
  root.dataset['theme'] = base;
  for (const key of THEME_VARS) root.style.removeProperty(`--${key}`);
  if (colors) {
    for (const key of THEME_KEYS) {
      const value = colors[key];
      if (value) root.style.setProperty(`--${key}`, value);
    }
    const accent = colors['accent'] ?? '#6366f1';
    root.style.setProperty('--accent-soft', withAlpha(accent, 0.16));
  }
}

export function baseOf(colors: ThemeColors): 'dark' | 'light' {
  const value = colors['bg'] ?? '#000000';
  const n = Number.parseInt(value.slice(1), 16);
  const lum = 0.2126 * ((n >> 16) & 255) + 0.7152 * ((n >> 8) & 255) + 0.0722 * (n & 255);
  return lum < 128 ? 'dark' : 'light';
}

export function readStandardColors(base: 'dark' | 'light'): ThemeColors {
  applyTheme(base, null);
  const style = getComputedStyle(document.documentElement);
  const toHex = (value: string): string => {
    const trimmed = value.trim();
    if (/^#[0-9a-f]{6}$/i.test(trimmed)) return trimmed.toLowerCase();
    if (/^#[0-9a-f]{3}$/i.test(trimmed)) {
      return '#' + trimmed.slice(1).split('').map((char) => char + char).join('').toLowerCase();
    }
    return '#000000';
  };
  const colors: ThemeColors = {};
  for (const key of THEME_KEYS) colors[key] = toHex(style.getPropertyValue(`--${key}`));
  colors['side'] = colors['panel'] ?? '#121a25';
  colors['btn'] = colors['panel2'] ?? '#18222f';
  colors['on-accent'] = '#ffffff';
  return colors;
}

interface ThemeStore {
  state: ThemeState;
  loadedFor: number | null;
  load: (userId: number) => Promise<void>;
  reset: () => void;
  applyActive: () => void;
  pick: (id: string) => Promise<string | null>;
  saveCustom: (editId: string | null, name: string, colors: ThemeColors) => Promise<string | null>;
  deleteCustom: (customId: string) => Promise<string | null>;
  savePreset: (presetId: string, name: string, colors: ThemeColors) => Promise<string | null>;
  deletePreset: (presetId: string) => Promise<string | null>;
  setStandard: (base: 'dark' | 'light') => Promise<void>;
}

const initial: ThemeState = { active: 'dark', custom: [] };

let catalogLoaded = false;
let catalogPromise: Promise<void> | null = null;

/** Loads the theme catalog from YAML files once per page. */
async function loadCatalog(): Promise<void> {
  if (catalogLoaded) return;
  if (!catalogPromise) {
    catalogPromise = authApi.getThemePresets()
      .then((catalog) => { setThemeCatalog(catalog); catalogLoaded = true; })
      .catch(() => { /* the server theme will remain the default; retry on the next login */ })
      .finally(() => { catalogPromise = null; });
  }
  await catalogPromise;
}

export const useThemeStore = create<ThemeStore>((set, get) => ({
  state: initial,
  loadedFor: null,
  load: async (userId) => {
    if (get().loadedFor === userId) return;
    // The catalog is needed before applying the theme: otherwise the preset from the saved profile
    // would momentarily fall back to the default palette.
    await loadCatalog();
    let state = initial;
    try {
      const response = await authApi.getTheme();
      if (response.theme) state = response.theme;
    } catch {
      state = initial;
    }
    set({ state, loadedFor: userId });
    const resolved = themeFromId(state.active, state);
    applyTheme(resolved.base, resolved.colors);
  },
  reset: () => {
    set({ state: initial, loadedFor: null });
    applyTheme('dark', null);
  },
  applyActive: () => {
    const resolved = themeFromId(get().state.active, get().state);
    applyTheme(resolved.base, resolved.colors);
  },
  pick: async (id) => {
    const state = { ...get().state, active: id };
    set({ state });
    const resolved = themeFromId(id, state);
    applyTheme(resolved.base, resolved.colors);
    try {
      const response = await authApi.putTheme(state);
      if (response.theme) set({ state: response.theme });
      return null;
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
  },
  saveCustom: async (editId, name, colors) => {
    const current = get().state;
    const list = current.custom.slice();
    let id = editId;
    if (id) {
      const index = list.findIndex((item) => item.id === id);
      if (index >= 0) {
        const existing = list[index];
        if (existing) list[index] = { ...existing, name, base: baseOf(colors), colors };
      }
    } else {
      if (list.length >= MAX_CUSTOM) return `Maximum ${MAX_CUSTOM} custom themes.`;
      id = 'c' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5);
      list.push({ id, name, base: baseOf(colors), colors });
    }
    const state: ThemeState = { active: `custom:${id}`, custom: list, presets: current.presets ?? {}, hidden: current.hidden ?? [] };
    set({ state });
    const resolved = themeFromId(state.active, state);
    applyTheme(resolved.base, resolved.colors);
    try {
      const response = await authApi.putTheme(state);
      if (response.theme) set({ state: response.theme });
      return null;
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
  },
  deleteCustom: async (customId) => {
    const current = get().state;
    const state: ThemeState = {
      active: current.active === `custom:${customId}` ? 'dark' : current.active,
      custom: current.custom.filter((item) => item.id !== customId),
      presets: current.presets ?? {},
      hidden: current.hidden ?? [],
    };
    set({ state });
    const resolved = themeFromId(state.active, state);
    applyTheme(resolved.base, resolved.colors);
    try {
      const response = await authApi.putTheme(state);
      if (response.theme) set({ state: response.theme });
      return null;
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
  },
  savePreset: async (presetId, name, colors) => {
    const current = get().state;
    const state: ThemeState = {
      ...current,
      active: `preset:${presetId}`,
      presets: { ...(current.presets ?? {}), [presetId]: { name, base: baseOf(colors), colors } },
      hidden: (current.hidden ?? []).filter((item) => item !== presetId),
    };
    set({ state });
    const resolved = themeFromId(state.active, state);
    applyTheme(resolved.base, resolved.colors);
    try {
      const response = await authApi.putTheme(state);
      if (response.theme) set({ state: response.theme });
      return null;
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
  },
  deletePreset: async (presetId) => {
    const current = get().state;
    const presets = { ...(current.presets ?? {}) };
    delete presets[presetId];
    const state: ThemeState = {
      active: current.active === `preset:${presetId}` ? 'dark' : current.active,
      custom: current.custom,
      presets,
      hidden: [...(current.hidden ?? []).filter((item) => item !== presetId), presetId],
    };
    set({ state });
    const resolved = themeFromId(state.active, state);
    applyTheme(resolved.base, resolved.colors);
    try {
      const response = await authApi.putTheme(state);
      if (response.theme) set({ state: response.theme });
      return null;
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
  },
  setStandard: async (base) => {
    const current = get().state;
    if (current.active === base) return;
    await get().pick(base);
  },
}));

export function listThemes(state: ThemeState): ResolvedTheme[] {
  const translate = (name: string): string => t(name as Parameters<typeof t>[0]);
  const standard: ResolvedTheme[] = themeCatalog.standard.map((item) => ({
    id: item.id, name: translate(item.name), base: item.base, colors: item.colors ?? null, kind: 'standard', sw: item.sw,
  }));
  const hidden = new Set(state.hidden ?? []);
  const presets: ResolvedTheme[] = themeCatalog.presets.filter((item) => !hidden.has(item.id)).map((item) => {
    const override = state.presets?.[item.id];
    return override
      ? { id: `preset:${item.id}`, name: override.name, base: override.base, colors: override.colors, kind: 'preset', sw: item.sw }
      : { id: `preset:${item.id}`, name: translate(item.name), base: item.base, colors: item.colors ?? null, kind: 'preset', sw: item.sw };
  });
  const custom: ResolvedTheme[] = state.custom.map((item: CustomTheme) => ({
    id: `custom:${item.id}`, name: item.name, base: item.base, colors: item.colors, kind: 'custom', cid: item.id,
  }));
  return [...standard, ...presets, ...custom];
}
