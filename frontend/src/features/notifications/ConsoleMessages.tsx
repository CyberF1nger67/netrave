import { useEffect } from 'react';
import { useAuthStore } from '../../stores/auth';
import { useNotificationsStore } from '../../stores/notifications';
import { MessagePopup } from './MessagePopup';

/**
 * Notification delivery for the console window, which is a separate page without the main shell.
 * It keeps the notification connection open while the user is signed in, so administrator messages
 * appear here too, and it shows the message dialog. The bell and the panel stay in the main window.
 */
export function ConsoleMessages() {
  const user = useAuthStore((state) => state.user);
  const start = useNotificationsStore((state) => state.start);
  const stop = useNotificationsStore((state) => state.stop);
  useEffect(() => {
    if (user) start();
    else stop();
    return () => stop();
  }, [user, start, stop]);
  return <MessagePopup />;
}
