import type { IconName } from '../../components/Icon';
import { t } from '../../i18n';
import type { NotificationCategory, NotificationItem } from '../../types/api';

export function catLabel(categories: NotificationCategory[], key: string): string {
  return categories.find((category) => category.key === key)?.label ?? t('System');
}

export function levelIcon(level: NotificationItem['level']): IconName {
  if (level === 'success') return 'success';
  if (level === 'warning') return 'warning';
  if (level === 'error') return 'error';
  return 'info';
}
