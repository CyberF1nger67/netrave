import { foldersApi, labsApi } from '../api/endpoints';
import type { Lab, LabFolder } from '../types/api';

const normalize = (value: string): string => value.trim().toLocaleLowerCase();

/** Looks up a folder by name, case-insensitive. */
export function findFolder(folders: LabFolder[], name: string): LabFolder | undefined {
  if (!name) return undefined;
  const wanted = normalize(name);
  return folders.find((folder) => normalize(folder.name) === wanted);
}

/** Looks up a lab by name inside a folder; names are compared case-insensitively. */
export function findLab(labs: Lab[], folderId: number, name: string): Lab | undefined {
  const wanted = normalize(name);
  return labs.find((lab) => (lab.folder_id || 0) === folderId && normalize(lab.name) === wanted);
}

/** Folder name for the URL; null means the root ("no folder"). */
export function folderNameOf(folders: LabFolder[], folderId: number): string | null {
  return folders.find((folder) => folder.id === folderId)?.name ?? null;
}

export const folderListQuery = { queryKey: ['folders'], queryFn: foldersApi.list } as const;
export const labListQuery = { queryKey: ['labs'], queryFn: () => labsApi.list() } as const;
