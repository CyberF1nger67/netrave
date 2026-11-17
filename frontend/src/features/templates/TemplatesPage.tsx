import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { imagesApi, templatesApi } from '../../api/endpoints';
import type { Template } from '../../types/api';
import { can, useAuthStore } from '../../stores/auth';
import { guard, toast } from '../../stores/toast';
import { Modal } from '../../components/ui/Modal';
import { EmptyState } from '../../components/ui/EmptyState';
import { Spinner } from '../../components/ui/Spinner';
import { Dropdown } from '../../components/ui/Dropdown';
import { NumberField } from '../../components/ui/NumberField';
import { IconPicker } from '../editor/IconPicker';
import type { IconSelection } from '../editor/IconPicker';
import { t, useLanguageStore } from '../../i18n';
import { withAlpha } from '../../lib/color';
import { VNC_BITRATE_PRESETS, VNC_ENCODINGS, VNC_RESOLUTIONS } from '../vnc/quality';
import type { VncQualitySettings } from '../../types/api';

const CATEGORIES = [
  { key: 'cisco', label: 'Cisco', tone: '#38bdf8' }, { key: 'mikrotik', label: 'MikroTik', tone: '#f59e0b' },
  { key: 'linux', label: 'Linux', tone: '#a3e635' }, { key: 'windows', label: 'Windows', tone: '#60a5fa' },
  { key: 'firewall', label: 'Firewall', tone: '#f87171' }, { key: 'network', label: 'Network and switching', tone: '#2dd4bf' },
  { key: 'server', label: 'Servers', tone: '#c084fc' }, { key: 'other', label: 'Other', tone: '#94a3b8' },
] as const;
const CAT_ICONS: Record<string, string> = {
  cisco: 'M12 4v3M8 7h8M7 10h10v4H7zM9 14v3M15 14v3M5 19h14',
  mikrotik: 'M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 8l4 2.3v4.6L12 17l-4-2.3v-4.6z',
  linux: 'M4 5h16v14H4zM7 9l3 3-3 3M12 15h5',
  windows: 'M4 5l7-1v7H4zM12 4l8-1v8h-8zM4 13h7v7l-7-1zM12 13h8v8l-8-1z',
  firewall: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM9 12l2 2 4-4',
  network: 'M3 9h18v6H3zM6 12h.01M9.5 12h.01M13 12h.01M16.5 12h.01M18 4v5M6 15v4M18 15v4',
  server: 'M4 5h16v5H4zM4 14h16v5H4zM8 7.5h.01M8 16.5h.01M12 7.5h4M12 16.5h4',
  other: 'M7 6h10v12H7zM9 9h6v6H9zM4 9h3M4 15h3M17 9h3M17 15h3M9 3v3M15 3v3M9 18v3M15 18v3',
};
const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';

const TYPES = ['linux', 'windows', 'router', 'switch', 'firewall', 'server', 'generic'];
const NICS = ['virtio-net-pci', 'e1000', 'e1000e', 'rtl8139', 'vmxnet3', 'i82559er', 'ne2k_pci'];

function categoryOf(template: Template): string {
  if (template.category) return template.category;
  const vendor = template.vendor.toLowerCase();
  if (vendor.includes('cisco')) return 'cisco';
  if (vendor.includes('mikrotik')) return 'mikrotik';
  if (vendor.includes('microsoft') || template.type === 'windows') return 'windows';
  if (template.type === 'firewall') return 'firewall';
  if (template.type === 'linux' || ['alpine', 'ubuntu', 'debian'].includes(vendor)) return 'linux';
  if (template.type === 'server') return 'server';
  if (template.type === 'router' || template.type === 'switch') return 'network';
  return 'other';
}

function TemplateDialog({ template, onClose, onSaved }: { template: Template | null; onClose: () => void; onSaved: () => void }) {
  const blankVnc: VncQualitySettings = { resolution: 'auto', bitrate: null, quality: null, compression: null, fps: null, encoding: 'auto' };
  const blank = { name: '', vendor: '', type: 'linux', category: '', arch: 'x86_64', machine: 'pc', cpu: 1, ram_mb: 1024, nics: 1, nic_model: 'virtio-net-pci', disk_bus: 'virtio', bios: 'seabios', console: 'serial', extra_args: '', iface_type: 'ens', iface_start: 0, image_id: 0, vnc: blankVnc };
  const [form, setForm] = useState(template ?? blank);
  // Machine icon: standard or a custom PNG from the library (as in machine settings).
  const [icon, setIcon] = useState<IconSelection>({ icon: template?.icon ?? '', custom_icon: template?.custom_icon ?? '' });
  const images = useQuery({ queryKey: ['images'], queryFn: () => imagesApi.list() });
  const set = (key: string, value: string | number) => setForm((current) => ({ ...current, [key]: value }));
  const setVnc = (patch: Partial<VncQualitySettings>) => setForm((current) => ({ ...current, vnc: { ...current.vnc, ...patch } }));
  return (
    <Modal title={template ? `${t('Template')}: ${template.name}` : t('New template')} onClose={onClose} wide actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: t('Save'), primary: true, onClick: async () => {
        await guard(async () => {
          const payload: Record<string, unknown> = { ...form, icon: icon.icon, custom_icon: icon.custom_icon };
          for (const key of ['cpu', 'ram_mb', 'nics', 'iface_start']) payload[key] = Number(payload[key]);
          if (template) await templatesApi.update(template.id, payload); else await templatesApi.create(payload);
          toast.success(t('Saved')); onSaved(); onClose();
        });
      } },
    ]}>
      <div className="grid grid-cols-1 gap-x-3.5 sm:grid-cols-2">
        <label>{t('Name')}<input value={form.name} maxLength={64} onChange={(event) => set('name', event.target.value)} /></label>
        <label>{t('Vendor')}<input value={form.vendor} maxLength={64} onChange={(event) => set('vendor', event.target.value)} /></label>
        <label>{t('Type')}<select value={form.type} onChange={(event) => set('type', event.target.value)}>{TYPES.map((type) => <option key={type}>{type}</option>)}</select></label>
        <label>{t('Catalog category')}<select value={form.category || categoryOf(form as Template)} onChange={(event) => set('category', event.target.value)}>{CATEGORIES.map((category) => <option key={category.key} value={category.key}>{category.label}</option>)}</select></label>
        <label>{t('Architecture')}<select value={form.arch} onChange={(event) => set('arch', event.target.value)}><option>x86_64</option><option>i386</option></select></label>
        <label>{t('QEMU machine')}<input value={form.machine} onChange={(event) => set('machine', event.target.value)} /></label>
        <label>{t('CPU')}<NumberField min={1} max={64} value={form.cpu} onValueChange={(value) => set('cpu', value)} /></label>
        <label>{t('RAM, MB')}<NumberField min={64} max={524288} value={form.ram_mb} onValueChange={(value) => set('ram_mb', value)} /></label>
        <label>{t('Network interfaces')}<NumberField min={1} max={32} value={form.nics} onValueChange={(value) => set('nics', value)} /></label>
        <label>{t('Network card')}<select value={form.nic_model} onChange={(event) => set('nic_model', event.target.value)}>{NICS.map((model) => <option key={model}>{model}</option>)}</select></label>
        <label>{t('Interface type')}<select value={form.iface_type} onChange={(event) => set('iface_type', event.target.value)}>{['ens', 'te', 'ge'].map((type) => <option key={type} value={type}>{type}</option>)}</select></label>
        <label>{t('Starting port')}<NumberField min={-1} max={31} title={t('-1 shifts numbering: nic0 = MGMT, nic1 = ge0')} value={form.iface_start} onValueChange={(value) => set('iface_start', value)} /></label>
        <label>{t('Disk bus')}<select value={form.disk_bus} onChange={(event) => set('disk_bus', event.target.value)}>{['virtio', 'ide', 'sata', 'scsi'].map((bus) => <option key={bus}>{bus}</option>)}</select></label>
        <label>{t('Firmware')}<select value={form.bios} onChange={(event) => set('bios', event.target.value)}><option value="seabios">{t('BIOS (SeaBIOS)')}</option><option value="uefi">{t('UEFI')}</option></select></label>
        <label>{t('Console')}<select value={form.console} onChange={(event) => set('console', event.target.value)}><option value="serial">{t('Serial (terminal)')}</option><option value="vnc">{t('VNC (graphics)')}</option></select></label>
        <label>{t('Additional QEMU arguments')}<input value={form.extra_args} maxLength={512} onChange={(event) => set('extra_args', event.target.value)} /></label>
        <label>{t('Disk image')}
          <Dropdown value={String(form.image_id ?? 0)} onChange={(value) => set('image_id', Number(value))}
            options={[{ value: '0', label: t('No image') }, ...(images.data ?? []).map((image) => ({ value: String(image.id), label: image.name }))]} />
        </label>
      </div>
      {form.console === 'vnc' && (
        <div className="mt-3 rounded-[14px] border border-line bg-panel2 px-3.5 py-3">
          <div className="mb-1.5 text-[13px] font-semibold">{t('Image quality (VNC)')}</div>
          <p className="mb-2 text-[12px] text-muted">{t('Defaults for new machines from this template. Auto keeps the value chosen by the console.')}</p>
          <div className="grid grid-cols-1 gap-x-3.5 sm:grid-cols-2">
            <label>{t('Resolution')}
              <select value={form.vnc.resolution} onChange={(event) => setVnc({ resolution: event.target.value as VncQualitySettings['resolution'] })}>
                {VNC_RESOLUTIONS.map((option) => <option key={option.value} value={option.value}>{option.value === 'auto' ? t('Auto') : option.label}</option>)}
              </select>
            </label>
            <label>{t('Encoding')}
              <select value={form.vnc.encoding} onChange={(event) => setVnc({ encoding: event.target.value as VncQualitySettings['encoding'] })}>
                {VNC_ENCODINGS.map((option) => <option key={option.value} value={option.value}>{t(option.label)}</option>)}
              </select>
            </label>
            <label>{t('Bitrate, Mbit/s')}
              <OptionalNumber min={1} max={100} value={form.vnc.bitrate} onChange={(value) => setVnc({ bitrate: value })} placeholder={t('Auto')} />
            </label>
            <label>{t('JPEG quality')}
              <OptionalNumber min={1} max={9} value={form.vnc.quality} onChange={(value) => setVnc({ quality: value })} placeholder={t('Auto')} />
            </label>
            <label>{t('Compression')}
              <OptionalNumber min={1} max={9} value={form.vnc.compression} onChange={(value) => setVnc({ compression: value })} placeholder={t('Auto')} />
            </label>
            <label>{t('Frame rate')}
              <OptionalNumber min={1} max={60} value={form.vnc.fps} onChange={(value) => setVnc({ fps: value })} placeholder={t('Auto')} />
            </label>
          </div>
          <div className="mt-2 flex flex-wrap gap-1">
            {VNC_BITRATE_PRESETS.map((preset) => (
              <button key={preset.value} type="button" className="cursor-pointer rounded-[6px] border border-line bg-transparent px-1.5 py-0.5 text-[11px] text-muted hover:text-text"
                onClick={() => setVnc({ bitrate: preset.value })} title={`${t(preset.label)} · ${preset.value} ${t('Mbit/s')}`}>
                {preset.value}
              </button>
            ))}
          </div>
        </div>
      )}
      <div className="mt-3 rounded-[14px] border border-line bg-panel2 px-3.5 py-3">
        <div className="mb-1.5 text-[13px] font-semibold">{t('Machine image')}</div>
        <IconPicker value={icon} onChange={setIcon} type={form.type} customUrl={template?.custom_icon_url ?? ''} />
      </div>
    </Modal>
  );
}

// OptionalNumber - a number or "Auto" (empty field). Out-of-range values are clamped.
function OptionalNumber({ value, min, max, onChange, placeholder }: { value: number | null; min: number; max: number; onChange: (value: number | null) => void; placeholder: string }) {
  return (
    <input
      type="number"
      inputMode="numeric"
      min={min}
      max={max}
      step={1}
      value={value ?? ''}
      placeholder={placeholder}
      onChange={(event) => {
        const raw = event.target.value;
        if (raw === '') { onChange(null); return; }
        const parsed = Number.parseInt(raw, 10);
        if (Number.isFinite(parsed)) onChange(Math.max(min, Math.min(max, parsed)));
      }}
    />
  );
}

type SortKey = 'name_asc' | 'name_desc' | 'added_new' | 'added_old';

const SORTS: Array<{ key: SortKey; label: string }> = [
  { key: 'name_asc', label: 'Name: A–Z' },
  { key: 'name_desc', label: 'Name: Z–A' },
  { key: 'added_new', label: 'Added: newest first' },
  { key: 'added_old', label: 'Added: oldest first' },
];

export function TemplatesPage() {
  const user = useAuthStore((state) => state.user);
  const queryClient = useQueryClient();
  const canEdit = can(user, 'templates');
  const query = useQuery({ queryKey: ['templates'], queryFn: templatesApi.list });
  const [search, setSearch] = useState('');
  const [type, setType] = useState('');
  const [vendor, setVendor] = useState('');
  const [arch, setArch] = useState('');
  const [sort, setSort] = useState<SortKey>('name_asc');
  const [dialog, setDialog] = useState<Template | null | 'new'>(null);
  useLanguageStore((state) => state.language);
  const list = query.data ?? [];

  const types = useMemo(() => [...new Set(list.map((template) => template.type))].sort(), [list]);
  const vendors = useMemo(() => [...new Set(list.map((template) => template.vendor).filter(Boolean))].sort((a, b) => a.localeCompare(b, 'ru')), [list]);
  const arches = useMemo(() => [...new Set(list.map((template) => template.arch))].sort(), [list]);

  const hasFilters = Boolean(search || type || vendor || arch);
  const resetFilters = () => { setSearch(''); setType(''); setVendor(''); setArch(''); };

  const needle = search.trim().toLowerCase();
  const filtered = list
    .filter((template) => {
      if (type && template.type !== type) return false;
      if (vendor && template.vendor !== vendor) return false;
      if (arch && template.arch !== arch) return false;
      if (!needle) return true;
      return [template.name, template.vendor, template.type, template.arch, template.machine]
        .some((field) => field.toLowerCase().includes(needle));
    })
    .sort((a, b) => {
      switch (sort) {
        case 'name_desc': return b.name.localeCompare(a.name, 'ru');
        // device_templates has no creation timestamp; id is AUTOINCREMENT, so
        // it reflects the order in which templates were added.
        case 'added_new': return b.id - a.id;
        case 'added_old': return a.id - b.id;
        default: return a.name.localeCompare(b.name, 'ru');
      }
    });

  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;

  return (
    <>
      <h2>{t('Device templates')}</h2>
      {canEdit && <div className="mb-3.5 flex flex-wrap items-center gap-2.5"><button type="button" className={primary} onClick={() => setDialog('new')}>{t('New template')}</button></div>}
      <div className="py-2 text-muted">{t('Template parameters are defaults for new machines.')}</div>
      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-col gap-4 rounded-app-sm border border-line bg-panel p-3 min-w-0">
          <label className="flex flex-col gap-1.5 min-w-0">
            <span className="text-xs font-semibold uppercase tracking-wide text-muted">{t('Search')}</span>
            <input type="search" className="w-full max-w-[520px]" placeholder={t('Search by name, vendor, type…')} value={search} onChange={(event) => setSearch(event.target.value)} />
          </label>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3 min-w-0">
            <label className="flex flex-col gap-1.5 min-w-0">
              <span className="text-xs font-semibold uppercase tracking-wide text-muted">{t('Device type')}</span>
              <select value={type} onChange={(event) => setType(event.target.value)}>
                <option value="">{t('All types')}</option>
                {types.map((item) => <option key={item} value={item}>{item}</option>)}
              </select>
            </label>
            <label className="flex flex-col gap-1.5 min-w-0">
              <span className="text-xs font-semibold uppercase tracking-wide text-muted">{t('Vendor')}</span>
              <select value={vendor} onChange={(event) => setVendor(event.target.value)}>
                <option value="">{t('All vendors')}</option>
                {vendors.map((item) => <option key={item} value={item}>{item}</option>)}
              </select>
            </label>
            <label className="flex flex-col gap-1.5 min-w-0">
              <span className="text-xs font-semibold uppercase tracking-wide text-muted">{t('Architecture')}</span>
              <select value={arch} onChange={(event) => setArch(event.target.value)}>
                <option value="">{t('All architectures')}</option>
                {arches.map((item) => <option key={item} value={item}>{item}</option>)}
              </select>
            </label>
          </div>
          <div className="flex flex-wrap items-end justify-between gap-3">
            <label className="flex flex-col gap-1.5 min-w-0">
              <span className="text-xs font-semibold uppercase tracking-wide text-muted">{t('Sort by')}</span>
              <select value={sort} onChange={(event) => setSort(event.target.value as SortKey)}>
                {SORTS.map((item) => <option key={item.key} value={item.key}>{t(item.label as Parameters<typeof t>[0])}</option>)}
              </select>
            </label>
            <button type="button" onClick={resetFilters} disabled={!hasFilters}>{t('Reset filters')}</button>
          </div>
          <div className="text-sm text-muted" aria-live="polite">
            {t('Showing')} {filtered.length} {t('of')} {list.length} {t('templates')}
          </div>
        </div>

        {filtered.length ? <div className="grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] items-stretch gap-3.5">{filtered.map((template) => {
          const cat = CATEGORIES.find((item) => item.key === categoryOf(template)) ?? CATEGORIES[CATEGORIES.length - 1]!;
          return (
            <div key={template.id} className="relative flex min-w-0 flex-col gap-2 overflow-hidden rounded-app border border-line bg-panel p-3.5 shadow-app transition-[transform,border-color,box-shadow] before:absolute before:inset-x-0 before:top-0 before:h-[3px] before:bg-[var(--tone)] before:opacity-85 before:content-['']" style={{ ['--tone' as string]: cat.tone }}>
              <div className="flex min-w-0 items-center gap-2"><span className="grid size-[42px] flex-none place-items-center rounded-xl border text-[var(--tone)]" style={{ borderColor: withAlpha(cat.tone, 0.3), background: withAlpha(cat.tone, 0.16) }}><svg viewBox="0 0 24 24" width={26} height={26} fill="none" stroke="currentColor" strokeWidth="1.8"><path d={CAT_ICONS[cat.key] ?? CAT_ICONS['other'] ?? ''} /></svg></span><span className="min-w-0 truncate text-[11.5px] font-semibold text-[var(--tone)]">{t(cat.label as Parameters<typeof t>[0])}</span></div>
              <div data-template-name className="truncate text-[15px] font-bold tracking-[-.01em]">{template.name}</div>
              <div className="truncate text-[12.5px] text-muted">{template.vendor || t('Vendor')} · {template.bios === 'uefi' ? 'UEFI' : 'BIOS'}</div>
              <dl className="mt-1 grid grid-cols-2 gap-x-2.5 gap-y-1.5">
                <div className="min-w-0"><dt className="text-[11px] text-muted">CPU</dt><dd className="m-0 truncate text-[13px] font-semibold tabular-nums">{template.cpu} {t('cores')}</dd></div><div className="min-w-0"><dt className="text-[11px] text-muted">RAM</dt><dd className="m-0 truncate text-[13px] font-semibold tabular-nums">{template.ram_mb >= 1024 ? `${(template.ram_mb / 1024).toFixed(template.ram_mb % 1024 ? 1 : 0)} ${t('GB')}` : `${template.ram_mb} ${t('MB')}`}</dd></div>
                <div className="min-w-0"><dt className="text-[11px] text-muted">{t('interfaces')}</dt><dd className="m-0 truncate text-[13px] font-semibold tabular-nums">{template.nics}</dd></div><div className="min-w-0"><dt className="text-[11px] text-muted">{t('Console')}</dt><dd className="m-0 truncate text-[13px] font-semibold tabular-nums">{template.console === 'vnc' ? 'VNC' : t('terminal')}</dd></div>
              </dl>
              {canEdit && <div className="mt-auto flex flex-wrap gap-1.5 pt-1.5 [&>button]:min-h-[30px] [&>button]:px-2.5 [&>button]:py-[5px] [&>button]:text-[12.5px]">
                <button type="button" onClick={() => setDialog(template)}>{t('Edit')}</button>
                {/* The duplicate gets all parameters, the disk and the icon of the source template. */}
                <button type="button" title={t('Create a copy of the template')} onClick={() => void guard(async () => {
                  const copy = await templatesApi.duplicate(template.id);
                  toast.success(`${t('Template copied:')} ${copy.name}`);
                  void queryClient.invalidateQueries({ queryKey: ['templates'] });
                })}>{t('Copy')}</button>
                <button type="button" className={danger} onClick={() => {
                  if (!confirm(`${t('Delete template?')} «${template.name}»`)) return;
                  void guard(async () => { await templatesApi.remove(template.id); void queryClient.invalidateQueries({ queryKey: ['templates'] }); });
                }}>{t('Delete')}</button>
              </div>}
            </div>
          );
        })}</div> : <EmptyState
          title={list.length ? t('Templates not found') : t('No templates yet')}
          text={list.length ? t('Try different filters or reset them.') : canEdit ? t('Create the first template.') : t('The administrator has not added templates yet.')}
          action={list.length && hasFilters ? <button type="button" onClick={resetFilters}>{t('Reset filters')}</button> : undefined}
        />}
      </div>
      {dialog && <TemplateDialog template={dialog === 'new' ? null : dialog} onClose={() => setDialog(null)}
        onSaved={() => void queryClient.invalidateQueries({ queryKey: ['templates'] })} />}
    </>
  );
}
