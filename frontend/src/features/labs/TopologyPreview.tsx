import type { Topology } from '../../types/api';
import { deviceIconSrc, iconFor } from '../../lib/deviceIcon';
import { t } from '../../i18n';

export function TopologyPreview({ topology }: { topology: Topology }) {
  const W = 300, H = 160, P = 24;
  const points = [
    ...topology.devices.map((device) => ({ x: device.x, y: device.y, id: `d${device.id}`, name: device.name, device: true, type: device.type, icon: device.icon, src: device.icon_url })),
    ...topology.networks.map((network) => ({ x: network.x, y: network.y, id: `n${network.id}`, name: network.name, device: false, type: network.type, icon: '', src: '' })),
  ];
  if (!points.length) return <span className="text-muted">{t('Empty topology')}</span>;
  const xs = points.map((point) => point.x), ys = points.map((point) => point.y);
  const minx = Math.min(...xs), maxx = Math.max(...xs), miny = Math.min(...ys), maxy = Math.max(...ys);
  const width = Math.max(1, maxx - minx), height = Math.max(1, maxy - miny);
  const k = Math.min((W - 2 * P) / width, (H - 2 * P) / height, 1.5);
  const ox = P + ((W - 2 * P) - width * k) / 2, oy = P + ((H - 2 * P) - height * k) / 2;
  const pos = new Map<string, { id: string; x: number; y: number; name: string; device: boolean; type: string; icon: string; src: string }>();
  for (const point of points) {
    pos.set(point.id, { id: point.id, x: ox + (point.x - minx) * k, y: oy + (point.y - miny) * k, name: point.name, device: point.device, type: point.type, icon: point.icon, src: point.src });
  }
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="block h-40 w-full" preserveAspectRatio="xMidYMid meet">
      {topology.connections.map((conn) => {
        const a = pos.get((conn.a_type === 'device' ? 'd' : 'n') + conn.a_id);
        const b = pos.get((conn.b_type === 'device' ? 'd' : 'n') + conn.b_id);
        return a && b ? <line key={conn.id} className="stroke-muted stroke-[1.2]" x1={a.x} y1={a.y} x2={b.x} y2={b.y} /> : null;
      })}
      {[...pos.values()].map((point) => (
        <g key={point.id}>
          {point.device ? (
            <image className="max-w-none" href={point.src || deviceIconSrc({ icon: point.icon, type: point.type })} x={point.x - 12} y={point.y - 12} width={24} height={24} />
          ) : point.type === 'nat' || point.type === 'cloud' ? (
            <image className="max-w-none" href={iconFor(point.type)} x={point.x - 17} y={point.y - 11} width={34} height={22} />
          ) : (
            <circle className="fill-[#2a4a5c]" cx={point.x} cy={point.y} r={7} />
          )}
          <text className="fill-muted text-[9px]" x={point.x} y={point.y + (point.device ? 22 : 17)} textAnchor="middle">{point.name}</text>
        </g>
      ))}
    </svg>
  );
}
