import { create } from 'zustand';
import { locales } from 'virtual:locales';

// Locales are YAML files: `frontend/locales/<lang>.yaml`, one `"English key": "translation"`
// entry per line. vite-plugins/yaml-locales.ts parses them at build time and exposes
// them as the `virtual:locales` module, so the bundle contains plain objects only.
//
// English is the source language, the default UI language and the type of every key:
// keys are English strings and `t()` returns the key itself for 'en'. That is why there
// is no `locales/en.yaml` — it would duplicate every key; only translated languages
// (currently `locales/ru.yaml`) need a file. A new key is added to EN_KEYS below and
// translated in the YAML files; missing translations fall back to the English key.

export const EN_KEYS = [
  'Page not found', 'This page does not exist or has moved.', 'Go to laboratories', 'Your session has expired. Sign in again to continue.',
  'Dashboard (system summary)', 'Templates: editing', 'Images: upload and delete', 'Icon library', 'No PNG files selected', 'No image files selected', 'Upload image', 'Upload an image or a whole folder.', 'Machine icons: PNG, JPEG, WEBP or SVG. Choose an icon when adding or editing a machine.', 'PNG, JPEG, WEBP or SVG', 'PNG, JPEG, WEBP or SVG required', 'SVG contains unsafe content', 'SVG has no size', 'Uploading', 'Yes', 'No', 'Loading…', 'Save', 'Cancel', 'Delete', 'Close', 'Create', 'Add', 'Edit', 'Search', 'Refresh',
  'Back', 'Open', 'Status', 'Error', 'Settings', 'Name', 'Description', 'Actions', 'Owner', 'Created',
  'Size', 'Type', 'State', 'CPU', 'RAM', 'Disk', 'Console', 'Network', 'Device', 'Machine', 'Laboratory',
  'User', 'Role', 'Password', 'Access', 'Permissions', 'Theme', 'Language', 'Logout', 'Account', 'Administrator',
  'User role', 'Dashboard', 'Users', 'Journal', 'Themes', 'Images', 'Icons', 'Templates', 'Running labs',
  'Server', 'Switch', 'Router', 'Desktop', 'Workstation', 'Laptop', 'Monitor', 'Linux server', 'Custom', 'Laboratories', 'System settings', 'Account:', 'New laboratory', 'New folder', 'Search by name and description…',
  'All laboratories', 'Folders', 'Laboratories in this folder', 'Nothing found', 'Folder is empty',
  'No laboratories yet', 'Open', 'Parameters', 'Move to folder…', 'Access permissions', 'Open my copy',
  'Copy', 'Export', 'Wipe Lab', 'Delete', 'Instances:', 'No instances', 'Owner:', 'Rights:',
  'Import', 'Import a laboratory from a NetRave export file (YAML)', 'Laboratory imported', 'Imported with warnings:', 'The file is too large (max. 8 MB)',
  'New user', 'Username', 'Password (≥ 8 characters)', 'Create', 'Role', 'Status', 'Created',
  'Make user', 'Make admin', 'Enable', 'Disable', 'Rights', 'Password', 'Delete user',
  'You', 'Disabled', 'Active', 'User', 'Administrator',
  'Session lifetime, hours', 'Maximum image upload size, MB', 'Save', 'Settings are saved',
  'Time', 'User', 'Action', 'Details', 'IP',
  'Clear journal', 'All journal entries will be deleted. This is irreversible.', 'Journal cleared', 'Journal is empty',
  'Current password', 'New password (8–128 characters)', 'Change', 'Password changed', 'New password must contain 8 to 128 characters',
  'Changing the password is disabled by the administrator',
  'View', 'Change Password', 'Confirm Password', 'Enter current password', 'Enter new password', 'Enter the same password again',
  'Passwords do not match', 'Save Changes', 'Password changed successfully', 'Password reset', 'Set a new password for {username}',
  'Change role', 'Role changed', 'Choose the account role. Administrators have full access to all sections.',
  'Title', 'Description', 'Create', 'Save', 'Cancel', 'Wipe laboratory',
  'Reset laboratory', 'All machines will be stopped and their state deleted. This is irreversible.',
  'Preserved: topology, cables, layout, source images and uploaded disks.',
  'Folder name', 'Location', 'Rename folder', 'Move folder', 'Move laboratory', 'Root (no folder)',
  'Permissions:', 'Interface sections', 'The administrator always has all sections.',
  'Rights for laboratories', 'Select a laboratory to configure machine permissions.',
  'The user owns this laboratory: all rights are always granted.',
  'The owner and administrators always have all rights.',
  'General access:', 'No (owner and explicitly granted rights only)', 'View for everyone',
  'Access saved', 'Saved', 'Close',
  'Running laboratories', 'Instances of laboratories with running machines. The list refreshes every 4 seconds.',
  'Search by laboratory name…', 'All owners', 'All', 'Running', 'Partial', 'Problems', 'Mine', 'Wiped', 'Paused', 'Starting', 'Stopping', 'Stopped',
  'Laboratories', 'Machines running', 'CPU load', 'Process RAM', 'Problems', 'of', 'cores',
  'owner', 'started by', 'unknown start time', 'machines', 'errors:', 'without process:', 'transition:',
  'Console', 'Start', 'Stop', 'Restart', 'Pause', 'Resume', 'Wipe', 'Hide machines', 'Machines',
  'Nothing is running', 'Start a laboratory and it will appear here.', 'Back to laboratories',
  'Machines of the instance', 'CPU and RAM are measured per QEMU process', 'Loading machines…',
  'Failed to load instance machines', 'No machines in the instance', 'works', 'paused', 'starting',
  'stopping', 'stopped', 'error',
  'Device templates', 'New template', 'Search by name, vendor, type…', 'Category', 'Device type', 'All types', 'All vendors', 'All architectures', 'Sort by', 'Reset filters', 'Showing', 'templates', 'Templates not found', 'Try different filters or reset them.', 'Browser did not allow reading the clipboard. Paste the text here and press Into VM.', 'Clipboard is empty', 'Not typed (unsupported on US layout):', 'Only Latin-1 characters can be sent to the VM clipboard. Use Into VM for ASCII text.', 'Too many attempts. Try again later.', 'Server error. Try again later.', 'The request was rejected. Reload the page and try again.', 'Name: A–Z', 'Name: Z–A', 'Added: newest first', 'Added: oldest first', 'All', 'Edit', 'Delete',
  'Built-in', 'Template parameters are defaults for new machines.',
  'No templates yet', 'Create the first template.', 'The administrator has not added templates yet.',
  'Name', 'Vendor', 'Type', 'Catalog category', 'Architecture', 'QEMU machine', 'CPU', 'RAM, MB',
  'Network interfaces', 'Network card', 'Interface type', 'Starting port', 'Disk bus', 'Firmware', 'Console', 'Additional QEMU arguments',
  'BIOS (SeaBIOS)', 'UEFI', 'Serial (terminal)', 'VNC (graphics)', 'Save', 'Cancel',
  'Images', 'Upload image (whole folder)', 'Upload image folder', 'Scan image directory',
  'Folder', 'Disks (in order)', 'CD-ROM', 'Size', 'Used', 'Used by', 'No disks',
  'No images yet', 'Upload a folder with disks.', 'Choose the folder that contains the image disks.', 'Image registered:', 'Added:', 'updated:',
  'Rename image', 'New image name:', 'Delete image and its folder?', 'Select image folder',
  'Icons', 'PNG icons of machines. Choose an icon when adding or editing a machine.',
  'Upload PNG', 'Upload folder', 'Delete selected', 'Preview', 'Transparency', 'Machines',
  'Icon library is empty', 'Upload a PNG or a whole folder.', 'Rename icon', 'Delete icon?',
  'Selected', 'Select all', 'Name', 'Size',
  'Theme settings', 'Choose a ready theme or create your own. Colors apply immediately and are saved for your account.',
  '+ Create theme', 'Apply theme', 'Standard', 'Ready-made theme', 'My theme', 'Dark', 'Light',
  'Ocean', 'Forest', 'Sunset', 'Graphite', 'Lavender', 'Mint', 'Aurora', 'Neon', 'Cyber', 'Ember',
  'Violet', 'Midnight', 'Candy', 'Sky', 'Peach', 'Lime',
  'Copy', 'Edit', 'Delete', 'Edit theme', 'New theme', 'Save theme', 'Cancel', 'Restore originals',
  'Changes are visible across the whole interface immediately.', 'Theme saved.', 'Could not save theme:',
  'Background and panels', 'Page background', 'Inputs', 'Panels and cards', 'Hover backgrounds',
  'Accent backgrounds', 'Sidebar', 'Buttons and borders', 'Buttons', 'Borders', 'Hover borders',
  'Text', 'Primary text', 'Muted text', 'Accents', 'Accent', 'Accent (gradient)', 'Text on accent buttons',
  'Statuses', 'Success', 'Error', 'Warning', 'Theme name',
  'Notifications', 'All', 'Read all', 'Clear read', 'Read', 'Show more', 'In this category there is nothing',
  'There are no notifications yet', 'Real time', 'No connection', 'Unread', 'Mark as read',
  'Mark as unread', 'Delete notification', 'Close',
  'Log in to NetRave', 'Sign in to manage laboratories and machines.', 'Username', 'Password',
  'Enter username', 'Enter password', 'Show password', 'Hide password', 'Caps Lock is on',
  'Sign in', 'Checking…', 'Invalid username or password.', 'No connection to the server. Check your connection and try again.',
  'Accounts are created by the system administrator',
  'Dashboard', 'Host resources', 'Memory:', 'Disk', 'Version', 'uptime', 'Environment checks',
  'Running machines', 'all laboratories and users', 'nothing is running now',
  'Total CPU load', 'cores · updated', 'No server connection',
  'Add machine', 'Machine parameters', 'Template', 'Image and quantity', 'No image selected',
  'Image', 'Quantity', 'Name / prefix', 'Resources', 'CPU cores', 'Network adapters', 'QEMU parameters',
  'Network card model', 'Disk bus', 'Firmware', 'Console', 'QEMU machine type', 'Additional QEMU arguments',
  'Machine image', 'Image quality (VNC)', 'Defaults for new machines from this template. Auto keeps the value chosen by the console.', 'Bitrate, Mbit/s', 'Choose a template on the left', 'Add', 'Cancel', 'No templates',
  'Device settings', 'Machine', 'Image', 'Network settings', 'Host interface',
  'Isolated', 'NAT', 'Cloud', 'General NAT cloud', 'Save', 'Cancel',
  'New connection', 'Interface', 'Connect', 'No free interfaces on one of the devices.',
  'Network-to-network connections are not supported.', 'Cable created', 'Cable created but not applied:',
  'Snapshots', 'Snapshot state', 'The machine is running and will be stopped before the operation.',
  'The machine is stopped.', 'Snapshot', 'New image', 'Original image',
  'Overwrite the original image with the current state?', 'Save to original image',
  'I understand: the original image will be overwritten', 'Create snapshot', 'Restore', 'Delete',
  'No snapshots', 'Start log', 'Refresh', 'Status', 'No data:', 'Machines running',
  'Disk, total', 'Occupied by all laboratory machines', 'Machine', 'CPU', 'RAM', 'Disk', 'PID',
  'CPU is the share of all host cores. RAM is the resident memory of QEMU processes.',
  'Failure', 'Save error', 'Saved', 'Saving…', 'No rights to delete the selection',
  'Delete selected objects?', 'The cable is connected to a running machine. It will be disconnected immediately. Delete?',
  'Copied:', 'Paste', 'Select all', 'Copy', 'Duplicate', 'Delete', 'Settings…', 'Snapshots…',
  'Start log…', 'Clone', 'Wipe (reset disk)…', 'Open terminal', 'Open screen (VNC)',
  'Force stop', 'No rights to clone', 'No rights to reset the disk', 'No rights to add objects',
  'No rights to change the cable', 'Network settings…', 'Delete connection', 'Disable cable',
  'Enable cable', 'Add network', 'Add machine', 'Tools', 'Start all', 'Stop all', 'Status',
  'Zoom in', 'Zoom out', 'Fit to screen', 'Enlarge icons', 'Shrink icons', 'Parameters', 'Access rights', 'Export', 'Wipe laboratory',
  'Laboratory', 'Administrator works in a user laboratory', 'full administrator access',
  'Terminal', 'VNC', 'Console', 'All terminals', 'Reconnect current', 'Full screen',
  'Current console in a new tab', 'Minimize', 'Close all', 'Close', 'All in a new tab',
  'Open consoles', 'Machines in the laboratory', 'No machines in the laboratory',
  'Machine stopped — console disconnected', 'Connection closed, reconnecting…',
  'Could not connect: the device is not running or the console is unavailable',
  'Connected', 'Connecting…', 'Error', 'Open in another window', 'Open here',
  'Keyboard is captured by the VM · click outside to release', 'Keyboard input is sent to the VM · click outside to return to the browser',
  'Fit', 'Actual', 'Auto', 'Font', 'Send Ctrl+Alt+Del to VM', 'Send Alt+Tab to VM',
  'Send Ctrl+Esc to VM', 'Send Alt+F4 to VM', 'Virtual keyboard (Ctrl+Alt+K)', 'VNC clipboard',
  'Virtual machine keyboard', 'Close', 'Clipboard', 'Text to paste into VM (multiple lines allowed)',
  'Into VM', 'Stop', 'Into VNC clipboard', 'From VM', 'Copy', 'Copied to browser clipboard',
  'Browser did not allow copying', 'Typing…', 'Characters typed:', 'Typing stopped',
  'Text received from VM', 'No text received from VM', 'Sent to VNC clipboard',
  'Virtual keyboard', 'Serial keyboard', 'Modifier', 'Terminal clipboard',
  'Paste is disabled by the administrator',
  'Image quality', 'VNC image quality', 'Current resolution', 'Resolution', 'Auto (fits the window)',
  'Bitrate', 'Mbit/s', 'Advanced', 'JPEG quality', 'Compression', 'Frame rate', 'FPS',
  'Encoding', 'Tight (compression/JPEG)', 'Hextile (lossless)', 'Raw (maximum compatibility)',
  'Apply', 'Reset', 'Default', 'Applied', 'Saving…', 'Pending changes — press Apply',
  'The resolution is being applied…', 'The resize request was forwarded to the guest',
  'The display adapter does not support changing the resolution', 'The server rejected the requested resolution',
  'Could not load the image settings', 'Could not save the image settings', 'Could not decode the image update',
  '1–100 Mbit/s is a target, not a network cap',
  'Economy', 'Low', 'Standard', 'High', 'Very high', 'Maximum',
  'Copy screen', 'Read clipboard', 'Clear', 'Text inserted into terminal',
  'Screen copied to the field', 'Could not read the clipboard', 'Text received from the browser',
  'Nothing to copy', 'Paste into VM',
  'Work', 'Resources', 'System', 'Administration', 'Profile', 'Toggle theme', 'Running laboratories of all users', 'Collapse / expand menu', 'Menu', 'NetRave',
  'Instance user', 'Failed to load laboratories', 'Subfolders', 'labs', 'Delete folder?', 'Drag to a folder to move', 'Separate copy only for you', 'Your copy of the laboratory has been created', 'Laboratory copied', 'Delete laboratory?', 'Change the query.', 'Drag a laboratory here or create a new one.', 'Create the first laboratory or folder for grouping.', 'Reset disk of machine?', 'Failed to load running laboratories', 'updated', 'instances', 'needs attention', 'no errors', 'QEMU processes', 'machines are stopped', 'Device is not running — start it to open the console', 'No rights to the console of this machine', 'Reset disk of device?', 'Loading editor…', 'Laboratory not found', 'Started', 'Rights', 'Add', 'Default console', 'Snapshot restored', 'Snapshot created', 'Enter a name', 'Original image overwritten', 'Restore snapshot?', 'Delete snapshot?', 'Network interface', 'Network-to-network connection is not supported', 'No free interfaces', 'Cable created but not applied', 'Cable connected', 'Cable disconnected', 'Subfolders:', 'machines', 'Total machines', 'Memory', 'all host cores', 'After restart', 'Firewall', 'Network and switching', 'Servers', 'Other', 'cores', 'GB', 'MB', 'interfaces', 'terminal', 'Delete template?',
  'Only your instance', 'No data', 'Remaining machines are stopped', 'No machines', 'Occupied by all machines', 'Calculating…', 'Updates in real time', 'Update on schedule', 'Categories', 'Events will appear here', 'Icon uploaded to the library', 'This icon already exists and was selected', 'PNG with transparent background', 'Delete read notifications', 'Standard:', 'New password for', 'Delete user with their laboratories?',
  'File is empty', 'File is larger than 512 KB', 'PNG file required', 'PNG could not be opened', 'Canvas 2D is unavailable', 'Unsupported encoding', 'Unknown server message', 'Download failed', 'Network error while uploading', 'Laboratory reset', 'Reset completed with errors', 'System', 'Empty topology',
  'Service status', 'Daemon status', 'Installation', 'Uninstall', 'Diagnostics', 'Logs',
  'No folder', 'Root', 'Laboratories without a folder', 'No laboratories available',
  'The administrator has not assigned any folders with laboratories to you yet.',
  'Laboratories will be moved to “No folder”.', 'Folder access', 'Assigned folders',
  'The administrator always has access to all folders.', 'Folder access saved',
  'Select the folders this user can see. Laboratories from other folders stay hidden.',
  'When a parent folder is assigned, its subfolders are available automatically.',
  'There are no folders yet. Create folders in the laboratory list.',
  'Roles', 'Select', 'Create a copy of the template', 'Template copied:', 'No role',
  'New role',
  'Role name',
  'Role saved',
  'Role deleted',
  'Delete role?',
  'Members',
  'Role members',
  'No roles yet',
  'No members yet',
  'Members added',
  'Add selected',
  'Remove',
  'Search users by login…',
  'No users found',
  'Save the role first, then add members.',
  'Each role receives only the permissions selected here.',
  'All role members automatically get access to the selected folders and their subfolders.',
  'Roles combine permissions and folder access for their members.',
  'Custom roles', 'Roles grant permissions and access to folders. Assign them here.',
  'No custom roles yet', 'No roles available', 'Assign roles to users in the Users section.',
  'New interface (hotplug)',
  'Cable disconnected, but the interface is still attached:',
  'MAC addresses', 'Automatic', 'Manual', 'assigned on save', 'Invalid MAC format: use XX:XX:XX:XX:XX:XX', 'MAC must be unicast (first byte even)', 'Zero MAC is not allowed', 'Could not load MAC addresses', 'MAC addresses are applied on the next start', 'Manual MAC addresses are set for a single machine only',
  '-1 shifts numbering: nic0 = MGMT, nic1 = ge0',
  'Interface is disabled or has no IP address', 'Interface is configured', 'Interface state is unknown', 'Interfaces and IP addresses…', 'No machines are connected to this network', 'Configure…',
  'Memory page merging: identical pages of virtual machines share one physical copy, saving RAM.',
  'Page merging enabled', 'Page merging disabled', 'Page merging is not available on this host', 'Saved by page merging', 'Page merging (KSM/UKSM)', 'page merging',
  'KSM and UKSM solve the same task: enabling one turns the other off automatically.',
  'Standard kernel page merging: always available, uses some CPU.',
  'Page merging with low CPU load; requires UKSM support in the kernel.', 'Unavailable', 'On', 'Off', 'enabled', 'disabled',
  'from role', 'Built-in', 'View', 'Disk image', 'No image', 'Running laboratories',
  'Maximum simultaneously running laboratories', '0 means no limit. Only laboratories with machines running at the same time are counted.',
  
  'Choose the role of the account. Every role grants its own permissions and folder access.',
  'Roles',
  'The administrator role always has every permission. Only the description can be changed.',
  'Search by user, action, details or IP…', 'All users', 'All actions', 'Period', 'All time', 'Last hour', 'Last 24 hours',
  'Last 7 days', 'Last 30 days', 'Sort', 'Newest first', 'Oldest first', 'Rows per page', 'Problems only', 'Shown', 'Next',
  'Connection is not ready: an interface is disabled or has no IP address',
] as const;

type TranslationKey = (typeof EN_KEYS)[number];

/** Messages of the language, or an empty object when the language has no file. */
function messages(language: string): Partial<Record<TranslationKey, string>> {
  return locales[language] ?? {};
}

// Languages offered by the switcher. Each one needs `locales/<code>.yaml` and a label.
export type Language = 'en' | 'ru';

const LANGUAGE_LABELS: Record<Language, string> = { en: 'English', ru: 'Русский' };

export function languageLabel(language: Language): string {
  return LANGUAGE_LABELS[language];
}

interface LanguageState {
  language: Language;
  setLanguage: (language: Language) => void;
}

export const useLanguageStore = create<LanguageState>((set) => ({
  language: (() => {
    try {
      return localStorage.getItem('netrave.language') === 'ru' ? 'ru' : 'en';
    } catch {
      return 'en';
    }
  })(),
  setLanguage: (language) => {
    try {
      localStorage.setItem('netrave.language', language);
    } catch {
      // localStorage may be unavailable
    }
    document.documentElement.lang = language;
    set({ language });
    window.dispatchEvent(new CustomEvent('netrave:language', { detail: language }));
  },
}));

export function currentLanguage(): Language {
  return useLanguageStore.getState().language;
}

export function t(key: TranslationKey, vars?: Record<string, string | number>): string {
  const language = currentLanguage();
  let text: string = language === 'en' ? key : (messages(language)[key] ?? key);
  if (vars) {
    for (const [name, value] of Object.entries(vars)) text = text.split(`{${name}}`).join(String(value));
  }
  return text;
}

export function setLanguage(language: Language): void {
  useLanguageStore.getState().setLanguage(language);
}

export function onLanguageChange(handler: (language: Language) => void): () => void {
  const listener = (event: Event) => handler((event as CustomEvent<Language>).detail);
  window.addEventListener('netrave:language', listener);
  return () => window.removeEventListener('netrave:language', listener);
}

// Keep the document language attribute in sync with the persisted selection.
if (typeof document !== 'undefined') {
  document.documentElement.lang = useLanguageStore.getState().language;
}
