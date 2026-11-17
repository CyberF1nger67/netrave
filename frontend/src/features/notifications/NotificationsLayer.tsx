import { useEffect } from 'react';
import { useAuthStore } from '../../stores/auth';
import { useNotificationsStore } from '../../stores/notifications';
import { NotificationsPanel } from './NotificationsPanel';

export function NotificationsLayer() {
  const user = useAuthStore((state) => state.user);
  const start = useNotificationsStore((state) => state.start);
  const stop = useNotificationsStore((state) => state.stop);
  useEffect(() => {
    if (user) start();
    else stop();
    return () => stop();
  }, [user, start, stop]);
  return <NotificationsPanel />;
}
