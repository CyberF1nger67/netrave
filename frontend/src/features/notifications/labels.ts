import type { IconName } from '../../components/Icon';
import { t, translateMessage } from '../../i18n';
import type { NotificationCategory, NotificationItem } from '../../types/api';

export function catLabel(categories: NotificationCategory[], key: string): string {
  const label = categories.find((category) => category.key === key)?.label;
  return label ? translateMessage(label) : t('System');
}

export function levelIcon(level: NotificationItem['level']): IconName {
  if (level === 'success') return 'success';
  if (level === 'warning') return 'warning';
  if (level === 'error') return 'error';
  return 'info';
}
