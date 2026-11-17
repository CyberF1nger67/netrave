import { t } from '../../i18n';

export function Spinner({ text }: { text?: string }) {
  return <div className="py-2 text-muted">{text ?? t('Loading…')}</div>;
}
