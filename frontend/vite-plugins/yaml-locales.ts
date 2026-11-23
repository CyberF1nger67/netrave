// Build-time loader for UI translations.
//
// Every `locales/<lang>.yaml` file becomes a property of the `virtual:locales`
// module: `export const locales: { [lang]: Record<string, string> }`. The files are
// parsed during the build, so no YAML parser and no YAML text reaches the bundle.
//
// Type declarations for the virtual module live in `src/vite-env.d.ts`.

import { readdir, readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { load } from 'js-yaml';
import type { Plugin } from 'vite';

const MODULE_ID = 'virtual:locales';
const RESOLVED_ID = '\0' + MODULE_ID;
const DEFAULT_DIR = 'locales';

interface Options {
  /** Directory with `*.yaml` files, relative to the Vite project root. */
  dir?: string;
}

/** Turns any parsed value into JSON that is valid inside a JS module. */
function toModuleValue(value: unknown): string {
  // U+2028 and U+2029 are line terminators in older JS engines; escape them for safety.
  return JSON.stringify(value).replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
}

/** Builds the error message shown when a locale file cannot be used. */
function localeError(file: string, message: string): Error {
  return new Error(`[yaml-locales] ${file}: ${message}`);
}

/** Reads and validates one locale file. Returns its language code and messages. */
async function loadLocale(dir: string, file: string): Promise<{ lang: string; messages: Record<string, string> }> {
  const path = join(dir, file);
  const lang = file.slice(0, -extname(file).length);
  let text: string;
  try {
    text = await readFile(path, 'utf8');
  } catch (error) {
    throw localeError(path, `cannot read file: ${(error as Error).message}`);
  }

  let data: unknown;
  try {
    // The default schema rejects duplicate keys, so a repeated key fails the build.
    data = load(text);
  } catch (error) {
    // js-yaml errors already carry the file name and the line/column position.
    throw localeError(path, `invalid YAML\n${(error as Error).message}`);
  }

  if (data === null || data === undefined) {
    throw localeError(path, 'the file is empty; expected "English key": "translation" entries');
  }
  if (typeof data !== 'object' || Array.isArray(data)) {
    throw localeError(path, 'the file must be a mapping of "English key": "translation" entries');
  }

  const messages: Record<string, string> = Object.create(null) as Record<string, string>;
  for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
    if (!key.trim()) throw localeError(path, 'an empty key is not allowed');
    if (typeof value !== 'string') {
      const kind = value === null ? 'null' : Array.isArray(value) ? 'a list' : typeof value;
      throw localeError(path, `the value of ${JSON.stringify(key)} is ${kind}; every translation must be a string`);
    }
    messages[key] = value;
  }
  return { lang, messages };
}

/** Vite plugin that emits the `virtual:locales` module from `locales/*.yaml`. */
export function yamlLocales(options: Options = {}): Plugin {
  const relativeDir = options.dir ?? DEFAULT_DIR;
  let dir = join(process.cwd(), relativeDir);

  return {
    name: 'netrave:yaml-locales',
    configResolved(config) {
      dir = join(config.root, relativeDir);
    },
    resolveId(id) {
      return id === MODULE_ID ? RESOLVED_ID : null;
    },
    async load(id) {
      if (id !== RESOLVED_ID) return null;

      let files: string[];
      try {
        files = (await readdir(dir)).filter((file) => extname(file) === '.yaml').sort();
      } catch (error) {
        this.error(`[yaml-locales] cannot read the locales directory ${dir}: ${(error as Error).message}`);
      }
      if (files.length === 0) this.error(`[yaml-locales] no *.yaml files found in ${dir}`);

      for (const file of files) this.addWatchFile(join(dir, file));

      const locales: Record<string, Record<string, string>> = Object.create(null) as Record<string, Record<string, string>>;
      for (const file of files) {
        const { lang, messages } = await loadLocale(dir, file);
        if (!/^[a-z]{2}(-[A-Za-z0-9]+)*$/.test(lang)) {
          this.error(`[yaml-locales] ${join(dir, file)}: "${lang}" is not a valid language code`);
        }
        locales[lang] = messages;
      }

      return `export const locales = ${toModuleValue(locales)};\n`;
    },
  };
}
