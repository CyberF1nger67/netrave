import { useEffect, useRef, useState } from 'react';
import { labsApi } from '../api/endpoints';
import type { ConsoleDevice } from '../features/consoles/types';
import { mountConsoleDock, setDeviceSource } from '../features/consoles/dock';
import { t } from '../i18n';
import { LanguageSwitcher } from '../components/LanguageSwitcher';
import type { ConsoleDock } from '../features/consoles/dock';

export function ConsoleApp() {
  const ref = useRef<ConsoleDock | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const labId = Number.parseInt(params.get('lab') ?? '', 10) || 0;
    const ids = (params.get('ids') ?? params.get('id') ?? '').split(',').map((value) => Number.parseInt(value, 10)).filter((value) => value > 0);
    const active = Number.parseInt(params.get('active') ?? '', 10) || ids[0] || 0;
    if (!ids.length) { setError(t('No machines in the laboratory')); return; }
    document.title = `${t('Console')} — NetRave`;
    const dock = mountConsoleDock(true);
    ref.current = dock;
    let latest: ConsoleDevice[] = [];
    setDeviceSource(() => latest);
    const adopt = (devices: ConsoleDevice[]) => {
      for (const id of ids) {
        const device = devices.find((item) => item.rid === id)
          ?? { rid: id, name: `${t('Device')} ${id}`, lab_id: labId, state: 'running', console: 'serial', lab_perms: {} } as ConsoleDevice;
        dock.open(device, true);
      }
      dock.activate(active);
    };
    if (labId) {
      const load = async () => {
        try {
          const topology = await labsApi.topology(labId);
          latest = topology.devices.map((device) => ({ rid: device.rid, name: device.name, lab_id: device.lab_id, state: device.state, console: device.console, lab_perms: topology.lab.perms }));
          setDeviceSource(() => latest);
          dock.syncStates();
          return latest;
        } catch {
          return latest;
        }
      };
      void load().then(adopt);
      const timer = window.setInterval(() => { void load().then((devices) => { if (!latest.length) adopt(devices); }); }, 4000);
      return () => { window.clearInterval(timer); };
    }
    adopt([]);
    return undefined;
  }, []);
  if (error) return <div style={{ color: '#fff', padding: 24 }}>{error}</div>;
  return <div className="fixed right-3 top-2 z-[200]"><LanguageSwitcher /></div>;
}
