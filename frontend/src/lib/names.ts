// Naming rules shared by the whole application (the backend enforces the same rules).
// Object names (labs, folders, templates, images, devices, networks, icons, themes): 1–64 characters
// that Linux allows in a file name: no '/', no control characters, no leading '.'.
// File names on disk: the same rule, at most 255 bytes.
// Roles and login names: 1–64 Latin letters, digits, '_' and '-', with single spaces between words.
// Passwords: 8–256 printable ASCII characters without spaces.

export const NAME_MAX_LENGTH = 64;
const FILE_NAME_MAX_BYTES = 255;
export const PASSWORD_MAX_LENGTH = 256;

const LOGIN_RE = /^[A-Za-z0-9_-]+( [A-Za-z0-9_-]+)*$/;
const PASSWORD_RE = /^[!-~]{8,256}$/;

/** Characters a path component may not contain: '/', control characters (C0 and C1), and a leading '.'. */
function hasLinuxNameChars(name: string): boolean {
  if (name === '' || name.startsWith('.')) return false;
  for (const ch of name) {
    const code = ch.codePointAt(0) ?? 0;
    if (ch === '/' || code < 0x20 || (code >= 0x7f && code <= 0x9f)) return false;
  }
  return true;
}

/** Length in characters (code points), as the server counts it. */
function charCount(name: string): number {
  return [...name].length;
}

/** A name of a folder, template, image, lab, device, network, icon or theme. */
export function isValidName(name: string): boolean {
  return charCount(name) <= NAME_MAX_LENGTH && hasLinuxNameChars(name);
}

/** A disk or ISO file name on disk: a name Linux allows, at most 255 bytes. */
export function isValidFileName(name: string): boolean {
  return new TextEncoder().encode(name).length <= FILE_NAME_MAX_BYTES && hasLinuxNameChars(name);
}

/** A role or login name: Latin letters, digits, '_' and '-', with single spaces between words. */
export function isValidLogin(name: string): boolean {
  return charCount(name) <= NAME_MAX_LENGTH && LOGIN_RE.test(name);
}

/** A password: 8–256 printable ASCII characters without spaces. */
export function isValidPassword(password: string): boolean {
  return PASSWORD_RE.test(password);
}
