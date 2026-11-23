import { foldersApi, labsApi } from '../api/endpoints';
import type { Lab, LabFolder } from '../types/api';

const normalize = (value: string): string => value.trim().toLocaleLowerCase();

/** Looks up a folder by name, case-insensitive. */
export function findFolder(folders: LabFolder[], nameOrPath: string): LabFolder | undefined {
  if (!nameOrPath) return undefined;
  const wanted = normalize(nameOrPath);
  return folders.find((folder) => normalize(folder.path) === wanted || normalize(folder.name) === wanted);
}

/** Looks up a lab by name; laboratory names are unique across the whole tree. */
export function findLab(labs: Lab[], name: string): Lab | undefined {
  const wanted = normalize(name);
  return labs.find((lab) => normalize(lab.name) === wanted);
}

/** Folder path for a lab ("" - the root). */
export function folderPathOf(lab: Lab): string {
  return lab.folder || '';
}

export const folderListQuery = { queryKey: ['folders'], queryFn: foldersApi.list } as const;
export const labListQuery = { queryKey: ['labs'], queryFn: () => labsApi.list() } as const;
