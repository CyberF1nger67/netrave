// Shared classes of the console virtual keyboard and clipboard panel.
export const KBD_ROOT = 'absolute bottom-2 left-1/2 z-30 box-border w-[min(600px,calc(100%-16px))] -translate-x-1/2 rounded-[10px] border border-line bg-panel2 p-2 text-xs shadow-[0_8px_24px_rgba(0,0,0,.35)]';
export const KBD_SERIAL_ROOT = `${KBD_ROOT} w-[min(680px,calc(100%-16px))]`;
export const KBD_HEAD = 'mb-1.5 flex items-center gap-2';
export const KBD_CLOSE = 'ml-auto px-2 py-0.5';
export const KBD_ROW = 'my-1 flex flex-wrap gap-1';
export const KBD_KEY = 'min-w-[34px] px-2 py-1 text-xs';
export const KBD_SERIAL_KEY = `${KBD_KEY} min-w-[38px]`;
export const KBD_ON = 'bg-accent text-white';
export const KBD_COMBO = 'font-semibold';
export const CLIP_ROOT = 'absolute right-2 top-2 z-30 box-border w-[min(380px,calc(100%-16px))] rounded-[10px] border border-line bg-panel2 p-2 text-xs shadow-[0_8px_24px_rgba(0,0,0,.35)]';
export const CLIP_SERIAL_ROOT = `${CLIP_ROOT} w-[min(680px,calc(100%-16px))]`;
export const CLIP_TEXT = 'box-border min-h-[110px] w-full resize-y font-mono text-xs';
export const CLIP_STATUS = 'mt-1 min-h-4 text-muted';
