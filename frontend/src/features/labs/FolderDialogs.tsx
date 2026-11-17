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
  value: number | null;
  exclude?: Set<number>;
  confirmLabel?: string;
  onSave: (folderId: number | null) => Promise<void>;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState(String(value ?? 0));
  const kids = new Map<number, LabFolder[]>();
  for (const folder of folders) {
    const key = folder.parent_id ?? 0;
    kids.set(key, [...(kids.get(key) ?? []), folder]);
  }
  for (const list of kids.values()) list.sort((a, b) => a.name.localeCompare(b.name, 'ru'));
  const rows: Array<{ folder: LabFolder; depth: number }> = [];
  const walk = (parent: number, depth: number) => {
    for (const folder of kids.get(parent) ?? []) {
      rows.push({ folder, depth }); walk(folder.id, depth + 1);
    }
  };
  walk(0, 0);
  return (
    <Modal title={title} onClose={onClose} actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: confirmLabel ?? t('Move folder'), primary: true, onClick: async () => { await onSave(Number(selected) || null); onClose(); } },
    ]}>
      <select value={selected} onChange={(event) => setSelected(event.target.value)}>
        <option value="0">{t('Root (no folder)')}</option>
        {rows.filter(({ folder }) => !exclude?.has(folder.id)).map(({ folder, depth }) => (
          <option key={folder.id} value={folder.id}>{'\u00a0\u00a0\u00a0'.repeat(depth) + (depth ? '↳ ' : '') + folder.name}</option>
        ))}
      </select>
    </Modal>
  );
}

export function subtreeIds(folders: LabFolder[], id: number): Set<number> {
  const ids = new Set<number>([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const folder of folders) {
      if (folder.parent_id && ids.has(folder.parent_id) && !ids.has(folder.id)) { ids.add(folder.id); grew = true; }
    }
  }
  return ids;
}

export function folderPath(folders: LabFolder[], id: number): LabFolder[] {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const path: LabFolder[] = [];
  let current = byId.get(id);
  while (current) {
    path.unshift(current);
    current = current.parent_id ? byId.get(current.parent_id) : undefined;
  }
  return path;
}
