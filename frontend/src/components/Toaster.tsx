import { useToastStore } from '../stores/toast';
import { Icon } from './Icon';
import type { IconName } from './Icon';
import { t } from '../i18n';

const tabClass: Record<string, string> = {
  success: 'border-l-ok',
  warning: 'border-l-warn',
  error: 'border-l-bad',
};

const iconOf = (level: string): IconName => {
  if (level === 'success') return 'success';
  if (level === 'warning') return 'warning';
  if (level === 'error') return 'error';
  return 'info';
};

export function Toaster() {
  const items = useToastStore((state) => state.items);
  const dismiss = useToastStore((state) => state.dismiss);
  if (!items.length) return null;
  return (
    <div className="fixed right-4 top-[76px] z-[160] max-[900px]:top-[70px] flex w-[min(380px,calc(100vw-32px))] flex-col gap-2" role="status" aria-live="polite">
      {items.map((item) => (
        <div key={item.id} className={`flex items-start gap-[9px] rounded-app-sm border border-line border-l-4 bg-panel px-3 py-[11px] text-[13px] leading-[1.45] text-text shadow-app ${tabClass[item.level]}`}>
          <Icon name={iconOf(item.level)} size={16} />
          <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">{item.message}</span>
          <button type="button" className="h-6 min-h-6 w-6 flex-none border-0 bg-transparent p-0 text-muted hover:bg-panel2 hover:text-text" aria-label={t('Close')} onClick={() => dismiss(item.id)}>
            <Icon name="error" size={12} />
          </button>
        </div>
      ))}
    </div>
  );
}
