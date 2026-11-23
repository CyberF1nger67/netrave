import { askConfirm } from '../../stores/dialogs';
import { useEffect, useMemo, useState } from 'react';
import { devicesApi, labsApi } from '../../api/endpoints';
import type { Connection, Device, DeviceCreateInput, ImageItem, Network, Template } from '../../types/api';
import { useAuthStore } from '../../stores/auth';
import { guard, toast } from '../../stores/toast';
import { ifaceName } from '../../lib/format';
import { Modal } from '../../components/ui/Modal';
import { Switch } from '../../components/ui/Switch';
import { IconPicker } from './IconPicker';
import { t, translateMessage } from '../../i18n';
import type { IconSelection } from './IconPicker';
import { NumberField } from '../../components/ui/NumberField';

const NIC_MODELS = ['virtio-net-pci', 'e1000', 'e1000e', 'rtl8139', 'vmxnet3', 'i82559er', 'ne2k_pci'];
const IFACE_TYPES = ['ens', 'te', 'ge'] as const;
const DISK_BUSES = ['virtio', 'ide', 'sata', 'scsi'];
const LABELS: Record<string, string> = {
  cpu: 'CPU', ram_mb: 'RAM, MB', nics: 'Network interfaces', nic_model: 'Network card',
  iface_type: 'Interface type', iface_start: 'Starting port',
  disk_bus: 'Disk bus',
  bios: 'Firmware', console: 'Default console', machine: 'QEMU machine', extra_args: 'Additional QEMU arguments',
};
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const hint = 'mt-2.5 text-[12.5px] leading-relaxed text-muted';
const errBox = 'my-2 max-h-[140px] overflow-auto whitespace-pre-wrap rounded-md border border-bad bg-bad/[.12] p-2 font-mono text-xs leading-snug';
const logBox = 'max-h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-bg p-2.5 font-mono text-xs leading-snug';
const DOT: Record<string, string> = {
  running: 'bg-ok', paused: 'bg-warn', starting: 'bg-accent', stopping: 'bg-accent', error: 'bg-bad',
};

function TemplatePicker({ templates, selected, onPick }: { templates: Template[]; selected: number | null; onPick: (template: Template) => void }) {
  const [search, setSearch] = useState('');
  const list = useMemo(() => templates.filter((template) => `${template.name} ${template.vendor} ${template.type}`.toLowerCase().includes(search.toLowerCase())).sort((a, b) => a.name.localeCompare(b.name, 'ru')), [templates, search]);
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="[&_input[type=search]]:max-w-[520px]"><input type="search" placeholder={t('Search')} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] items-stretch gap-2.5">
        {list.map((template) => (
          <div key={template.id} className={`relative flex min-w-0 cursor-pointer flex-col gap-1.5 overflow-hidden rounded-app border bg-panel p-[11px] shadow-app transition-[transform,border-color,box-shadow] duration-200 before:absolute before:inset-x-0 before:top-0 before:h-[3px] before:bg-accent before:opacity-85 before:content-[''] hover:-translate-y-[3px] hover:border-accent focus-visible:-translate-y-[3px] focus-visible:border-accent ${selected === template.id ? "border-accent shadow-[0_0_0_2px_var(--accent),0_14px_34px_-22px_var(--accent)] after:absolute after:right-2 after:top-2.5 after:grid after:size-5 after:place-items-center after:rounded-full after:bg-accent after:text-xs after:font-bold after:text-white after:content-['✓']" : 'border-line'}`} role="button" tabIndex={0}
            onClick={() => onPick(template)} onKeyDown={(event) => { if (event.key === 'Enter') onPick(template); }}>
            <div className="flex min-w-0 items-center gap-2"><span className="grid size-[34px] flex-none place-items-center rounded-xl border border-accent/30 bg-accent/[.16] text-accent"><span className="text-muted">{template.type.slice(0, 2).toUpperCase()}</span></span><span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] font-semibold text-accent">{template.category || template.type}</span></div>
            <div className="overflow-hidden text-ellipsis whitespace-nowrap text-sm font-bold tracking-[-.01em]">{template.name}</div>
            <div className="overflow-hidden text-ellipsis whitespace-nowrap text-[12.5px] text-muted">{template.vendor || t('Vendor')}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function AddMachineDialog({ labId, x, y, templates, images, onClose, onDone }: {
  labId: string;
  x: number;
  y: number;
  templates: Template[];
  images: ImageItem[];
  onClose: () => void;
  onDone: (devices: Device[]) => void;
}) {
  const [template, setTemplate] = useState<Template | null>(null);
  const [form, setForm] = useState({
    name: '', count: 1, cpu: 1, ram_mb: 1024, nics: 1, nic_model: NIC_MODELS[0] ?? 'virtio-net-pci',
    iface_type: 'ens' as (typeof IFACE_TYPES)[number], iface_start: 0,
    disk_bus: 'virtio', bios: 'seabios', console: 'serial', machine: 'pc', extra_args: '',
  });
  const [imageId, setImageId] = useState('');
  const [icon, setIcon] = useState<IconSelection>({ icon: '', custom_icon: '' });
  const set = (key: string, value: string | number) => setForm((current) => ({ ...current, [key]: value }));

  const pick = (next: Template) => {
    setTemplate(next);
    setForm({
      name: '', count: form.count, cpu: next.cpu, ram_mb: next.ram_mb, nics: next.nics, nic_model: next.nic_model,
      iface_type: IFACE_TYPES.includes(next.iface_type as (typeof IFACE_TYPES)[number]) ? next.iface_type : 'ens',
      iface_start: Number.isInteger(next.iface_start) ? next.iface_start : 0,
      disk_bus: next.disk_bus, bios: next.bios, console: next.console, machine: next.machine || 'pc', extra_args: next.extra_args || '',
    });
    // The disk and icon are set in the template: substitute them immediately, along with image parameters (machine, firmware, etc.).
    if (next.image_id) {
      const image = images.find((item) => item.id === next.image_id);
      applyImage(image?.dirname ?? '');
    } else {
      setImageId('');
    }
    setIcon({ icon: next.icon ?? '', custom_icon: next.custom_icon ?? '' });
  };

  const chooseImage = (id: string) => {
    applyImage(id);
    setImageId(id);
  };
  const applyImage = (dirname: string) => {
    setImageId(dirname);
    const image = images.find((item) => item.dirname === dirname);
    if (!image) return;
    // An image without QEMU parameters may arrive without a qemu object (null): use an empty dictionary.
    const qemu: Record<string, string> = image.qemu ?? {};
    setForm((current) => ({
      ...current,
      machine: qemu['machine'] ?? current.machine,
      bios: qemu['bios'] ?? current.bios,
      disk_bus: qemu['disk_bus'] ?? current.disk_bus,
      console: qemu['console'] ?? current.console,
      extra_args: qemu['extra_args'] ?? current.extra_args,
    }));
  };
  const submit = async () => {
    if (!template) { toast.error(t('Choose a template on the left')); return; }
    const input: DeviceCreateInput = {
      template_id: template.id, x, y, count: Number(form.count),
      ...(form.name.trim() ? { name: form.name.trim() } : {}),
      cpu: Number(form.cpu), ram_mb: Number(form.ram_mb), nics: Number(form.nics),
      nic_model: form.nic_model, iface_type: form.iface_type, iface_start: Number(form.iface_start),
      disk_bus: form.disk_bus, bios: form.bios, console: form.console as 'serial' | 'vnc',
      machine: form.machine.trim() || 'pc', extra_args: form.extra_args.trim(),
      ...(icon.icon ? { icon: icon.icon } : {}), ...(icon.custom_icon ? { custom_icon: icon.custom_icon } : {}),
      ...(imageId ? { image_id: imageId } : {}),
    };
    await guard(async () => {
      const created = await labsApi.createDevice(labId, input);
      toast.success(`${t('Added:')} ${created.length}`);
      onDone(created); onClose();
    });
  };

  return (
    <Modal title={t('Add machine')} onClose={onClose} wide actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: t('Add'), primary: true, onClick: submit },
    ]}>
      <div className="grid items-start gap-[22px] [grid-template-columns:minmax(0,1.15fr)_minmax(320px,1fr)] max-[860px]:grid-cols-1">
        <div className="min-w-0"><h4 className="mb-2.5 mt-0 text-xs font-bold uppercase tracking-[.06em] text-muted">1 · {t('Template')}</h4><TemplatePicker templates={templates} selected={template?.id ?? null} onPick={pick} /></div>
        <div className="min-w-0">
          <h4 className="mb-2.5 mt-0 text-xs font-bold uppercase tracking-[.06em] text-muted">2 · {t('Parameters')}</h4>
          <div className="flex max-h-[68vh] flex-col gap-3 overflow-auto p-0.5 max-[860px]:max-h-none">
            <div className={`flex items-center gap-3 rounded-[14px] border px-3.5 py-3 ${template ? 'border-accent/40 bg-accent/[.12]' : 'justify-center border-dashed border-line bg-panel2 text-[13px] text-muted'}`}>{template ? <b className="text-[15px]">{template.name}</b> : t('Choose a template on the left')}</div>
            <div className="rounded-[14px] border border-line bg-panel2 px-3.5 py-3 [&>div+div]:mt-2">
              <h4 className="mb-2 mt-0 text-[13px] font-bold">{t('Image and quantity')}</h4>
              <select value={imageId} onChange={(event) => chooseImage(event.target.value)}>
                <option value="">— {t('No image selected')} —</option>
                {images.map((image) => <option key={image.id} value={image.dirname}>{image.name} · {t('Disks (in order)')}: {image.disks.length}</option>)}
              </select>
              <div className="grid grid-cols-[150px_1fr] gap-3 max-[860px]:grid-cols-1">
                <label>{t('Quantity')}<NumberField min={1} max={20} value={form.count} onValueChange={(value) => set('count', value)} /></label>
                <label>{t('Name / prefix')}<input value={form.name} maxLength={32} onChange={(event) => set('name', event.target.value)} /></label>
              </div>
            </div>
            <div className="rounded-[14px] border border-line bg-panel2 px-3.5 py-3 [&>div+div]:mt-2">
              <h4 className="mb-2 mt-0 text-[13px] font-bold">{t('Resources')}</h4>
              <div className="grid grid-cols-2 gap-x-3.5 max-[900px]:grid-cols-1">
                <label>{t('CPU cores')}<NumberField min={1} max={64} value={form.cpu} onValueChange={(value) => set('cpu', value)} /></label>
                <label>{t('RAM, MB')}<NumberField min={64} max={524288} value={form.ram_mb} onValueChange={(value) => set('ram_mb', value)} /></label>
                <label>{t('Network adapters')}<NumberField min={1} max={32} value={form.nics} onValueChange={(value) => set('nics', value)} /></label>
                <label>{t('Interface type')}<select value={form.iface_type} onChange={(event) => set('iface_type', event.target.value)}>{IFACE_TYPES.map((type) => <option key={type} value={type}>{type}</option>)}</select></label>
                <label>{t('Starting port')}<NumberField min={-1} max={31} title={t('-1 shifts numbering: nic0 = MGMT, nic1 = ge0')} value={form.iface_start} onValueChange={(value) => set('iface_start', value)} /></label>
              </div>
              <div className="text-muted">
                {Array.from({ length: Math.min(3, Math.max(1, Number(form.nics))) }, (_, index) => ifaceName(form, index)).join(', ')}
                {Number(form.nics) > 3 ? ' …' : ''}
              </div>
            </div>
            <div className="rounded-[14px] border border-line bg-panel2 px-3.5 py-3 [&>div+div]:mt-2">
              <h4 className="mb-2 mt-0 text-[13px] font-bold">{t('QEMU parameters')}</h4>
              <div className="grid grid-cols-2 gap-x-3.5 max-[900px]:grid-cols-1">
                <label>{t('Network card model')}<select value={form.nic_model} onChange={(event) => set('nic_model', event.target.value)}>{NIC_MODELS.map((model) => <option key={model}>{model}</option>)}</select></label>
                <label>{t('Disk bus')}<select value={form.disk_bus} onChange={(event) => set('disk_bus', event.target.value)}>{DISK_BUSES.map((bus) => <option key={bus}>{bus}</option>)}</select></label>
                <label>{t('Firmware')}<select value={form.bios} onChange={(event) => set('bios', event.target.value)}><option value="seabios">{t('BIOS (SeaBIOS)')}</option><option value="uefi">{t('UEFI')}</option></select></label>
                <label>{t('Console')}<select value={form.console} onChange={(event) => set('console', event.target.value)}><option value="serial">{t('xterm.js')}</option><option value="vnc">{t('VNC (graphics)')}</option></select></label>
                <label>{t('QEMU machine type')}<input value={form.machine} maxLength={32} onChange={(event) => set('machine', event.target.value)} /></label>
              </div>
              <label>{t('Additional QEMU arguments')}<input value={form.extra_args} maxLength={512} onChange={(event) => set('extra_args', event.target.value)} /></label>
            </div>
            <details className="rounded-[14px] border border-line bg-panel2 px-3.5 py-3 [&[open]>summary]:mb-1.5"><summary className="cursor-pointer text-[13px] font-semibold">{t('Machine image')}</summary><IconPicker value={icon} onChange={setIcon} type={template?.type ?? 'generic'} customUrl={template?.custom_icon_url ?? (template?.custom_icon ? `/api/icon-library/${template.custom_icon}` : '')} /></details>
          </div>
        </div>
      </div>
    </Modal>
  );
}

const NUMERIC_MAX: Record<'cpu' | 'ram_mb' | 'nics' | 'iface_start', number> = { cpu: 64, ram_mb: 524288, nics: 32, iface_start: 31 };

export function DeviceSettingsDialog({ labName, device, images, onClose, onSaved }: { labName: string; device: Device; images: ImageItem[]; onClose: () => void; onSaved: (device: Device) => void }) {
  const running = device.state !== 'stopped' && device.state !== 'error';
  const [form, setForm] = useState({
    name: device.name, cpu: device.cpu, ram_mb: device.ram_mb, nics: device.nics, nic_model: device.nic_model,
    iface_type: device.iface_type, iface_start: device.iface_start,
    disk_bus: device.disk_bus, bios: device.bios, console: device.console, machine: device.machine, extra_args: device.extra_args,
  });
  const [imageId, setImageId] = useState(device.image_id ?? '');
  const [icon, setIcon] = useState<IconSelection>({ icon: device.icon, custom_icon: device.custom_icon });
  const set = (key: string, value: string | number) => setForm((current) => ({ ...current, [key]: value }));
  const pending = new Set(device.pending ?? []);

  const save = async () => {
    const payload: Record<string, unknown> = { ...form };
    for (const key of ['cpu', 'ram_mb', 'nics', 'iface_start']) payload[key] = Number(form[key as 'cpu' | 'ram_mb' | 'nics' | 'iface_start']);
    if (icon.icon !== device.icon || icon.custom_icon !== device.custom_icon) { payload['icon'] = icon.icon; payload['custom_icon'] = icon.custom_icon; }
    if (!running) {
      const next = imageId === '' ? null : imageId;
      if (next !== device.image_id) payload['image_id'] = next;
    }
    await guard(async () => {
      const saved = await labsApi.updateDevice(labName, device.name, payload);
      onSaved(saved);
      toast.success(t('Saved')); onClose();
    });
  };

  return (
    <Modal title={`${t('Device settings')}: ${device.name}`} onClose={onClose} wide actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: t('Save'), primary: true, onClick: save },
    ]}>
      <div className="mt-2.5 flex items-center gap-2 text-[13px]">
        <span className={`size-2.5 rounded-full ${DOT[device.state] ?? 'bg-[#6b7686]'}`} /><b>{device.state}</b>
        {device.pid ? ` · PID ${device.pid}` : ''}{device.vnc_port ? ` · VNC ${device.vnc_port}` : ''}{device.serial_port ? ` · serial ${device.serial_port}` : ''}
      </div>
      {device.last_error && <pre className={errBox}>{device.last_error}</pre>}
      <label>{t('Name')}<input value={form.name} maxLength={32} onChange={(event) => set('name', event.target.value)} /></label>
      <label>{t('Machine image')}</label>
      <IconPicker value={icon} onChange={setIcon} type={device.type} customUrl={device.icon_url} />
      <label>{t('Image')}<select value={imageId} disabled={running} onChange={(event) => setImageId(event.target.value)}>
        <option value="">— {t('No image selected')} —</option>
        {images.map((image) => <option key={image.id} value={image.dirname}>{image.name} ({t('Disks (in order)')}: {image.disks.length}{image.cdroms.length ? `, CD: ${image.cdroms.length}` : ''})</option>)}
      </select></label>
      <div className="grid grid-cols-2 gap-x-3.5 max-[900px]:grid-cols-1">
        {(['cpu', 'ram_mb', 'nics', 'nic_model', 'iface_type', 'iface_start', 'disk_bus', 'bios', 'console', 'machine', 'extra_args'] as const).map((key) => (
          <label key={key}>{t(LABELS[key] as Parameters<typeof t>[0])}{running && pending.has(key) ? ' · '+t('After restart') : ''}
            {key === 'nic_model' || key === 'disk_bus' || key === 'iface_type' ? (
              <select value={String(form[key])} onChange={(event) => set(key, event.target.value)}>
                {(key === 'iface_type' ? IFACE_TYPES : key === 'nic_model' ? NIC_MODELS : DISK_BUSES).map((option) => <option key={option}>{option}</option>)}
              </select>
            ) : key === 'bios' || key === 'console' ? (
              <select value={String(form[key])} onChange={(event) => set(key, event.target.value)}>
                {key === 'bios' ? <><option value="seabios">{t('BIOS (SeaBIOS)')}</option><option value="uefi">{t('UEFI')}</option></> : <><option value="serial">{t('xterm.js')}</option><option value="vnc">{t('VNC (graphics)')}</option></>}
              </select>
            ) : key === 'cpu' || key === 'ram_mb' || key === 'nics' || key === 'iface_start' ? (
              <NumberField min={key === 'iface_start' ? -1 : 1} max={NUMERIC_MAX[key]} value={form[key]} onValueChange={(value) => set(key, value)} />
            ) : (
              <input type="text" value={String(form[key])} onChange={(event) => set(key, event.target.value)} />
            )}
          </label>
        ))}
      </div>
      {running && <div className={hint}>{t('The machine is running and will be stopped before the operation.')}</div>}
    </Modal>
  );
}

export function NetworkSettingsDialog({ labName, network, onClose, onSaved }: { labName: string; network: Network; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(network.name);
  const [host, setHost] = useState(network.host_iface);
  return (
    <Modal title={`${t('Network settings')}: ${network.name} (${network.type === 'isolated' ? t('Isolated') : network.type === 'nat' ? t('NAT') : t('Cloud')})`}
      onClose={onClose} actions={[{ label: t('Cancel'), onClick: onClose }, { label: t('Save'), primary: true, onClick: async () => {
        await guard(async () => {
          await labsApi.updateNetwork(labName, network.name, { name, ...(network.type === 'cloud' ? { host_iface: host } : {}) });
          onSaved(); onClose();
        });
      } }]}>
      <label>{t('Name')}<input value={name} maxLength={32} onChange={(event) => setName(event.target.value)} /></label>
      {network.type === 'nat' && <div className="py-2 text-muted">{t('General NAT cloud')}: {network.subnet}, {network.gateway}, {network.dhcp_start} – {network.dhcp_end}</div>}
      {network.type === 'cloud' && <label>{t('Host interface')}<input value={host} placeholder="eth0, br0" onChange={(event) => setHost(event.target.value)} /></label>}
    </Modal>
  );
}

export function ConnectDialog({ labId, a, b, devices, networks, connections, onClose, onConnected }: {
  labId: string; a: string; b: string; devices: Device[]; networks: Network[]; connections: Connection[]; onClose: () => void; onConnected: (connection: Connection) => void;
}) {
  const parse = (key: string) => ({ kind: key[0] === 'd' ? 'device' as const : 'network' as const, id: key.slice(1) });
  const pa = parse(a), pb = b ? parse(b) : null;
  const free = (entry: { kind: 'device' | 'network'; id: string }): number[] => {
    if (entry.kind === 'network') return [0];
    const device = devices.find((item) => item.id === entry.id);
    if (!device) return [];
    const used = new Set<number>();
    for (const connection of connections) {
      if (connection.a_type === 'device' && connection.a_id === device.id) used.add(connection.a_if);
      if (connection.b_type === 'device' && connection.b_id === device.id) used.add(connection.b_if);
    }
    return Array.from({ length: device.nics }, (_, index) => index).filter((index) => !used.has(index));
  };
  // When dragging "machine -> network" the click goes to one machine, so the first end is
  // the device and the second is the network; the server accepts this order as is.
  // Each wire between the same machines must have its own physical interface:
  // occupied ports are not offered, and when none are free a new one is added.
  const fa = free(pa), fb = pb ? free(pb) : [];
  const deviceOf = (entry: { kind: string; id: string }) => devices.find((item) => item.id === entry.id);
  const optionsFor = (entry: { kind: string; id: string }, list: number[]): number[] => {
    const device = deviceOf(entry);
    const out = [...list];
    if (device && device.nics < 32 && !out.includes(device.nics)) out.push(device.nics);
    return out;
  };
  const oa = optionsFor(pa, fa), ob = pb ? optionsFor(pb, fb) : [];
  // The end with a device is connected via a free interface. If there are no free ports,
  // a new interface is offered: the first machine of the pair always has such a port available.
  const [ifA, setIfA] = useState(String(oa[0] ?? 0));
  const [ifB, setIfB] = useState(String(ob[0] ?? 0));
  // The drag direction does not affect the result: the machine always goes first, the network second,
  // so both sides create the same request to the server.
  const first = pa.kind === 'device' ? pa : pb ?? pa;
  const second = pa.kind === 'device' ? pb : pa;
  const firstIfaces = pa.kind === 'device' ? oa : ob;
  const secondIfaces = pa.kind === 'device' ? ob : oa;
  const firstNic = pa.kind === 'device' ? ifA : ifB;
  const setFirstNic = pa.kind === 'device' ? setIfA : setIfB;
  const secondNic = pa.kind === 'device' ? ifB : ifA;
  const setSecondNic = pa.kind === 'device' ? setIfB : setIfA;
  const name = (entry: { kind: string; id: string }) => entry.kind === 'device' ? devices.find((device) => device.id === entry.id)?.name ?? '?' : networks.find((network) => network.id === entry.id)?.name ?? `${t('Network')} #${entry.id}`;
  if (pb && pa.kind === 'network' && pb.kind === 'network') {
    return <Modal title={t('New connection')} onClose={onClose} actions={[{ label: t('Close'), onClick: onClose }]}><div className="text-bad">{t('Network-to-network connection is not supported')}</div></Modal>;
  }
  if (!oa.length || (pb && !ob.length)) {
    return <Modal title={t('New connection')} onClose={onClose} actions={[{ label: t('Close'), onClick: onClose }]}>
      <div className="text-bad">{t('No free interfaces')}</div>
    </Modal>;
  }
  // All free interfaces plus "new port" (a number equal to the current NIC count).
  const ifaceOptions = (entry: { kind: string; id: string }, list: number[]) => list.map((index) => {
    const device = deviceOf(entry);
    const added = device != null && index >= device.nics;
    const label = ifaceName(device, index);
    return <option key={index} value={index}>{added ? `+ ${label} · ${t('New interface (hotplug)')}` : label}</option>;
  });
  return (
    <Modal title={t('New connection')} onClose={onClose} actions={[{ label: t('Cancel'), onClick: onClose }, { label: t('Connect'), primary: true, onClick: async () => {
      await guard(async () => {
        const connection = await labsApi.createConnection(labId, {
          a_type: first.kind, a_name: first.id, a_if: first.kind === 'device' ? Number(firstNic) : 0,
          b_type: second?.kind ?? first.kind, b_name: second?.id ?? first.id, b_if: second && second.kind === 'device' ? Number(secondNic) : 0,
        });
        if (connection.warnings?.length) connection.warnings.forEach((warning) => toast.warning(`${t('Cable created but not applied')}: ${translateMessage(warning.key, warning.params)}`));
        else toast.success(t('Cable created'));
        onConnected(connection); onClose();
      });
    } }]}>
      {/* The order is the same for any drag: first the machine, then the network or the second machine. */}
      <label>{name(first)}{first.kind === 'device' ? ' — '+t('Network interface') : ''}
        {first.kind === 'device' ? <select value={firstNic} onChange={(event) => setFirstNic(event.target.value)}>{ifaceOptions(first, firstIfaces)}</select> : <span className="text-muted">{t('Network')} «{name(first)}»</span>}
      </label>
      {second && second.id !== first.id && <label>{name(second)}{second.kind === 'device' ? ' — '+t('Network interface') : ''}
        {second.kind === 'device' ? <select value={secondNic} onChange={(event) => setSecondNic(event.target.value)}>{ifaceOptions(second, secondIfaces)}</select> : <span className="text-muted">{t('Network')} «{name(second)}»</span>}
      </label>}
    </Modal>
  );
}

export function SaveImageDialog({ device, onClose, onChanged }: { device: Device; onClose: () => void; onChanged: () => void }) {
  const user = useAuthStore((state) => state.user);
  const [imageName, setImageName] = useState('');
  const [confirmOriginal, setConfirmOriginal] = useState(false);
  const live = device.state === 'running' || device.state === 'paused';

  const commit = async (mode: 'image' | 'original', title: string, question: string) => {
    if (!(await askConfirm({ title: question, message: live ? t('The machine is running and will be stopped before the operation.') : undefined }))) return;
    await guard(async () => {
      await devicesApi.commit(device.rid, mode, title, live);
      toast.success(t('Saved')); onChanged(); onClose();
    });
  };

  return (
    <Modal title={`${t('Save image…')}: ${device.name}`} onClose={onClose} wide actions={[{ label: t('Close'), onClick: onClose }]}>
      <div className="mt-3.5 border-t border-line pt-1.5">
        <div className={hint}>{live ? t('The machine is running and will be stopped before the operation.') : t('The machine is stopped.')}</div>
        <div className="my-2 rounded-md border border-line-2 p-2">
          <div className="mb-1.5 text-[13px]"><b>{t('New image')}</b> — {t('New image')}</div>
          <div className="my-1.5 flex items-center gap-1.5"><input value={imageName} maxLength={100} placeholder={t('New image')} onChange={(event) => setImageName(event.target.value)} />
            <button type="button" onClick={() => { if (!imageName.trim()) { toast.error(t('Enter a name')); return; } void commit('image', imageName.trim(), `${t('New image')} «${imageName.trim()}»?`); }}>{t('New image')}</button></div>
        </div>
        {user?.role === 'admin' && (
          <div className="my-2 rounded-md border border-[#c0392b] p-2">
            <div className="mb-1.5 text-[13px]"><b>{t('Original image')}</b> — {t('Original image')}</div>
            <div className="my-1.5"><Switch checked={confirmOriginal} label={t('I understand: the original image will be overwritten')} onChange={setConfirmOriginal} /></div>
            <button type="button" className={danger} disabled={!confirmOriginal} onClick={() => void commit('original', '', `${t('Original image')} «${device.name}»?`)}>{t('Save to original image')}</button>
          </div>
        )}
      </div>
    </Modal>
  );
}

export function LogDialog({ device, onClose }: { device: Device; onClose: () => void }) {
  const [text, setText] = useState(t('Loading…'));
  const load = () => devicesApi.log(device.rid).then((value) => setText(value || '(empty log)')).catch(() => setText(t('Failed to load instance machines')));
  useEffect(() => { void load(); }, [device.rid]);
  return (
    <Modal title={`${t('Start log')}: ${device.name}`} onClose={onClose} wide actions={[
      { label: t('Refresh'), onClick: () => { void load(); } }, { label: t('Close'), onClick: onClose },
    ]}>
      <pre className={logBox}>{text}</pre>
    </Modal>
  );
}

