import { useState } from 'react';
import type { LabFolder } from '../../types/api';
import { Modal } from '../../components/ui/Modal';
import { t } from '../../i18n';

export function FolderNameDialog({ title, initial = '', onSave, onClose }: { title: string; initial?: string; onSave: (name: string) => Promise<void>; onClose: () => void }) {
  const [name, setName] = useState(initial);
  return (
    <Modal title={title} onClose={onClose} actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: t('Save'), primary: true, onClick: async () => { if (name.trim()) await onSave(name.trim()); onClose(); } },
    ]}>
      <label htmlFor="folder-name">{t('Name')}</label><input id="folder-name" value={name} maxLength={80} onChange={(event) => setName(event.target.value)} />
    </Modal>
  );
}

export function FolderSelectDialog({ title, folders, value, exclude, confirmLabel, onSave, onClose }: {
  title: string;
  folders: LabFolder[];
  value: string | null;
  exclude?: Set<string>;
  confirmLabel?: string;
  onSave: (folderPath: string | null) => Promise<void>;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState(value ?? '');
  const kids = new Map<string, LabFolder[]>();
  for (const folder of folders) {
    const key = folder.parent;
    kids.set(key, [...(kids.get(key) ?? []), folder]);
  }
  for (const list of kids.values()) list.sort((a, b) => a.name.localeCompare(b.name, 'ru'));
  const rows: Array<{ folder: LabFolder; depth: number }> = [];
  const walk = (parent: string, depth: number) => {
    for (const folder of kids.get(parent) ?? []) {
      rows.push({ folder, depth }); walk(folder.path, depth + 1);
    }
  };
  walk('', 0);
  return (
    <Modal title={title} onClose={onClose} actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: confirmLabel ?? t('Move folder'), primary: true, onClick: async () => { await onSave(selected || null); onClose(); } },
    ]}>
      <select value={selected} onChange={(event) => setSelected(event.target.value)}>
        <option value="">{t('Root (no folder)')}</option>
        {rows.filter(({ folder }) => !exclude?.has(folder.path)).map(({ folder, depth }) => (
          <option key={folder.path} value={folder.path}>{'\u00a0\u00a0\u00a0'.repeat(depth) + (depth ? '↳ ' : '') + folder.name}</option>
        ))}
      </select>
    </Modal>
  );
}

export function subtreeIds(folders: LabFolder[], path: string): Set<string> {
  const ids = new Set<string>([path]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const folder of folders) {
      if (folder.parent && ids.has(folder.parent) && !ids.has(folder.path)) { ids.add(folder.path); grew = true; }
    }
  }
  return ids;
}

export function folderPath(folders: LabFolder[], path: string): LabFolder[] {
  const byPath = new Map(folders.map((folder) => [folder.path, folder]));
  const out: LabFolder[] = [];
  let current = byPath.get(path);
  while (current) {
    out.unshift(current);
    current = current.parent ? byPath.get(current.parent) : undefined;
  }
  return out;
}
