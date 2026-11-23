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
  'Icon library', 'No image files selected', 'Upload image', 'Upload an image or a whole folder.',
  'Machine icons: PNG, JPEG, WEBP or SVG. Choose an icon when adding or editing a machine.', 'PNG, JPEG, WEBP or SVG',
  'PNG, JPEG, WEBP or SVG required', 'SVG contains unsafe content', 'SVG has no size', 'Uploading', 'Yes', 'No', 'Loading…', 'Save', 'Cancel',
  'Delete', 'Close', 'Create', 'Add', 'Edit', 'Search', 'Refresh', 'Back', 'Open', 'Status', 'Error', 'Settings', 'Name', 'Description', 'Actions',
  'Owner', 'Created', 'Size', 'Type', 'State', 'CPU', 'RAM', 'Disk', 'Console', 'Network', 'Device', 'Machine', 'Laboratory', 'User', 'Role',
  'Password', 'Access', 'Permissions', 'Theme', 'Language', 'Logout', 'Account', 'Administrator', 'User role', 'Dashboard', 'Users', 'Journal',
  'Themes', 'Images', 'Icons', 'Templates', 'Running labs', 'Server', 'Switch', 'Router', 'Desktop', 'Workstation', 'Laptop', 'Monitor',
  'Linux server', 'Custom', 'Laboratories', 'System settings', 'New laboratory', 'New folder', 'Search by name and description…', 'Folders',
  'Laboratories in this folder', 'Nothing found', 'Folder is empty', 'No laboratories yet', 'Parameters', 'Move to folder…', 'Access permissions',
  'Open my copy', 'Copy', 'Export', 'Wipe Lab', 'Instances:', 'No instances', 'Owner:', 'Import',
  'Import a laboratory from a NetRave export file (YAML)', 'Laboratory imported', 'Imported with warnings:', 'The file is too large (max. 8 MB)',
  'New user', 'Username', 'Username: 1–64 characters, Latin letters, digits, _, - and single spaces between words', 'Password (≥ 8 characters)', 'Enable', 'Disable', 'Rights',
  'Delete user', 'You', 'Disabled', 'Active', 'Session lifetime, hours', 'Maximum image upload size, MB', 'Settings are saved', 'Time', 'Action',
  'Details', 'IP', 'Clear journal', 'All journal entries will be deleted. This is irreversible.', 'Journal cleared', 'Journal is empty',
  'Current password', 'New password (8–256 characters)', 'Change', 'Password changed',
  'Password: 8–256 characters, Latin letters, digits and symbols, no spaces', 'Changing the password is disabled by the administrator', 'View',
  'Change Password', 'Confirm Password', 'Enter current password', 'Enter new password', 'Enter the same password again', 'Passwords do not match',
  'Save Changes', 'Password changed successfully', 'Password reset', 'Set a new password for {username}', 'Change role', 'Role changed', 'Title',
  'Wipe laboratory', 'Reset laboratory', 'All machines will be stopped and their state deleted. This is irreversible.',
  'Preserved: topology, cables, layout, source images and uploaded disks.', 'Folder name', 'Location', 'Rename folder', 'Move folder',
  'Move laboratory', 'Root (no folder)', 'Permissions:', 'The administrator always has all sections.', 'Rights for laboratories',
  'Select a laboratory to configure machine permissions.', 'The user owns this laboratory: all rights are always granted.',
  'The owner and administrators always have all rights.', 'General access:', 'No (owner and explicitly granted rights only)', 'View for everyone',
  'Access saved', 'Saved', 'Running laboratories', 'Instances of laboratories with running machines. The list refreshes every 4 seconds.',
  'Search by laboratory name…', 'All', 'Running', 'Partial', 'Problems', 'Mine', 'Wiped', 'Paused', 'Starting', 'Stopping', 'Stopped',
  'Machines running', 'CPU load', 'Process RAM', 'of', 'cores', 'owner', 'unknown start time', 'machines', 'errors:', 'without process:',
  'transition:', 'Start', 'Stop', 'Restart', 'Pause', 'Resume', 'Wipe', 'Hide machines', 'Machines', 'Nothing is running',
  'Start a laboratory and it will appear here.', 'Back to laboratories', 'Machines of the instance', 'CPU and RAM are measured per QEMU process',
  'Loading machines…', 'Failed to load instance machines', 'No machines in the instance', 'works', 'paused', 'starting', 'stopping', 'stopped',
  'error', 'Device templates', 'New template', 'Search by name, vendor, type…', 'Category', 'Device type', 'All types', 'All vendors',
  'All architectures', 'Sort by', 'Reset filters', 'Showing', 'templates', 'Templates not found', 'Try different filters or reset them.',
  'Browser did not allow reading the clipboard. Paste the text here and press Into VM.', 'Clipboard is empty',
  'Not typed (unsupported on US layout):', 'Only Latin-1 characters can be sent to the VM clipboard. Use Into VM for ASCII text.',
  'Too many attempts. Try again later.', 'Server error. Try again later.', 'The request was rejected. Reload the page and try again.', 'Name: A–Z',
  'Name: Z–A', 'Added: newest first', 'Added: oldest first', 'Built-in', 'Template parameters are defaults for new machines.', 'No templates yet',
  'Create the first template.', 'The administrator has not added templates yet.', 'Vendor', 'Catalog category', 'Architecture', 'QEMU machine',
  'RAM, MB', 'Network interfaces', 'Network card', 'Interface type', 'Starting port', 'Disk bus', 'Firmware', 'Additional QEMU arguments',
  'BIOS (SeaBIOS)', 'UEFI', 'xterm.js', 'VNC (graphics)', 'Upload image (whole folder)', 'Upload image folder', 'Scan image directory',
  'Folder', 'Disks (in order)', 'CD-ROM', 'Used', 'Used by', 'No disks', 'No images yet', 'Upload a folder with disks.',
  'Choose the folder that contains the image disks.', 'Image registered:', 'Added:', 'updated:', 'New image name:', 'Delete image and its folder?',
  'Select image folder', 'Upload folder', 'Delete selected', 'Preview', 'Transparency', 'Icon library is empty', 'Rename icon', 'Delete icon?',
  'Selected', 'Select all', 'Theme settings', 'Choose a ready theme or create your own. Colors apply immediately and are saved for your account.',
  '+ Create theme', 'Apply theme', 'Standard', 'Ready-made theme', 'My theme', 'Dark', 'Light', 'Ocean', 'Forest', 'Sunset', 'Graphite', 'Lavender',
  'Mint', 'Aurora', 'Neon', 'Cyber', 'Ember', 'Violet', 'Midnight', 'Candy', 'Sky', 'Peach', 'Lime', 'Edit theme', 'New theme', 'Save theme',
  'Restore originals', 'Changes are visible across the whole interface immediately.', 'Theme saved.', 'Could not save theme:',
  'Background and panels', 'Page background', 'Inputs', 'Panels and cards', 'Hover backgrounds', 'Accent backgrounds', 'Sidebar',
  'Buttons and borders', 'Buttons', 'Borders', 'Hover borders', 'Text', 'Primary text', 'Muted text', 'Accents', 'Accent', 'Accent (gradient)',
  'Text on accent buttons', 'Statuses', 'Success', 'Warning', 'Theme name', 'Notifications', 'Read all', 'Clear read', 'Read', 'Show more',
  'There are no notifications yet', 'Real time', 'No connection', 'Unread', 'Mark as read', 'Mark as unread', 'Delete notification',
  'Log in to NetRave', 'Sign in to manage laboratories and machines.', 'Enter username', 'Enter password', 'Show password', 'Hide password',
  'Caps Lock is on', 'Sign in', 'Checking…', 'Invalid username or password.', 'No connection to the server. Check your connection and try again.',
  'Host resources', 'Memory:', 'Version', 'uptime', 'Environment checks', 'Running machines', 'all laboratories and users', 'nothing is running now',
  'Total CPU load', 'Add machine', 'Template', 'Image and quantity', 'No image selected', 'Image', 'Quantity', 'Name / prefix', 'Resources',
  'CPU cores', 'Network adapters', 'QEMU parameters', 'Network card model', 'QEMU machine type', 'Machine image', 'Image quality (VNC)',
  'Defaults for new machines from this template. Auto keeps the value chosen by the console.', 'Bitrate, Mbit/s', 'Choose a template on the left',
  'No templates', 'Device settings', 'Network settings', 'Host interface', 'Isolated', 'NAT', 'Cloud', 'General NAT cloud', 'New connection',
  'Interface', 'Connect', 'Cable created', 'The machine is running and will be stopped before the operation.', 'The machine is stopped.',
  'New image', 'Original image', 'Save to original image', 'Save image…',
  'I understand: the original image will be overwritten', 'Start log', 'Disk, total', 'PID',
  'CPU is the share of all host cores. RAM is the resident memory of QEMU processes.', 'Save error', 'Saving…', 'No rights to delete the selection',
  'Delete selected objects?', 'The cable is connected to a running machine. It will be disconnected immediately. Delete?', 'Copied:', 'Paste',
  'Duplicate', 'Settings…', 'Start log…', 'Clone', 'Wipe (reset disk)…', 'Open terminal', 'Open screen (VNC)', 'Force stop', 'No rights to clone',
  'No rights to reset the disk', 'No rights to add objects', 'Network settings…', 'Delete connection', 'Disable cable', 'Enable cable',
  'Add network', 'Tools', 'Start all', 'Stop all', 'Zoom in', 'Zoom out', 'Fit to screen', 'Terminal', 'VNC', 'All terminals', 'Full screen',
  'Full screen (session)', 'Exit full screen', 'Current console in a new tab', 'Minimize', 'All in a new tab', 'No machines in the laboratory',
  'Connected', 'Open in another window', 'Open here', 'Fit', 'Auto', 'Font', 'Send Ctrl+Alt+Del to VM', 'Virtual keyboard (Ctrl+Alt+K)',
  'VNC clipboard', 'Virtual machine keyboard', 'Clipboard', 'Text to paste into VM (multiple lines allowed)', 'Into VM', 'Into VNC clipboard',
  'From VM', 'Copied to browser clipboard', 'Browser did not allow copying', 'Typing…', 'Characters typed:', 'Typing stopped',
  'Text received from VM', 'No text received from VM', 'Sent to VNC clipboard', 'Virtual keyboard', 'Serial keyboard', 'Modifier',
  'Terminal clipboard', 'Paste is disabled by the administrator', 'Image quality', 'VNC image quality', 'Current resolution', 'Resolution',
  'Auto (fits the window)', 'Bitrate', 'Mbit/s', 'Advanced', 'JPEG quality', 'Compression', 'Frame rate', 'FPS', 'Encoding',
  'Tight (compression/JPEG)', 'Hextile (lossless)', 'Raw (maximum compatibility)', 'Apply', 'Reset', 'Default', 'Applied',
  'Pending changes — press Apply', 'The resolution is being applied…', 'The resize request was forwarded to the guest',
  'The display adapter does not support changing the resolution', 'The server rejected the requested resolution',
  'Could not load the image settings', 'Could not save the image settings', 'Could not decode the image update',
  '1–100 Mbit/s is a target, not a network cap', 'Economy', 'Low', 'High', 'Very high', 'Maximum', 'Copy screen', 'Read clipboard', 'Clear',
  'Text inserted into terminal', 'Screen copied to the field', 'Could not read the clipboard', 'Text received from the browser', 'Nothing to copy',
  'Paste into VM', 'Work', 'System', 'Administration', 'Profile', 'Toggle theme', 'Running laboratories of all users', 'Collapse / expand menu',
  'Menu', 'NetRave', 'Instance user', 'Failed to load laboratories', 'Subfolders', 'labs', 'Delete folder?', 'Drag to a folder to move',
  'Separate copy only for you', 'Your copy of the laboratory has been created', 'Laboratory copied', 'Delete laboratory?', 'Change the query.',
  'Drag a laboratory here or create a new one.', 'Create the first laboratory or folder for grouping.', 'Reset disk of machine?',
  'Failed to load running laboratories', 'updated', 'instances', 'needs attention', 'no errors', 'QEMU processes', 'machines are stopped',
  'Device is not running — start it to open the console', 'No rights to the console of this machine', 'Reset disk of device?', 'Loading editor…',
  'Laboratory not found', 'Started', 'Default console', 'Enter a name', 'Network interface', 'Network-to-network connection is not supported',
  'No free interfaces', 'Cable created but not applied', 'Cable connected', 'Cable disconnected', 'Memory', 'all host cores', 'After restart',
  'Firewall', 'Network and switching', 'Servers', 'Other', 'GB', 'MB', 'interfaces', 'terminal', 'Delete template?', 'Only your instance', 'No data',
  'Remaining machines are stopped', 'No machines', 'Occupied by all machines', 'Calculating…', 'Updates in real time', 'Update on schedule',
  'Events will appear here', 'Icon uploaded to the library', 'This icon already exists and was selected', 'Delete read notifications', 'Standard:',
  'Delete user with their laboratories?', 'File is empty', 'File is larger than 512 KB', 'PNG could not be opened', 'Canvas 2D is unavailable',
  'Unsupported encoding', 'Unknown server message', 'Download failed', 'Network error while uploading', 'Laboratory reset',
  'Reset completed with errors', 'Empty topology', 'Logs', 'No folder', 'Root', 'Laboratories without a folder', 'No laboratories available',
  'The administrator has not assigned any folders with laboratories to you yet.', 'Laboratories will be moved to “No folder”.', 'Folder access',
  'Assigned folders', 'The administrator always has access to all folders.', 'Folder access saved',
  'Select the folders this user can see. Laboratories from other folders stay hidden.',
  'When a parent folder is assigned, its subfolders are available automatically.',
  'There are no folders yet. Create folders in the laboratory list.', 'Roles', 'Select', 'Create a copy of the template', 'Template copied:',
  'No role', 'New role', 'Role name', 'Role saved', 'Role deleted', 'Delete role?', 'Members', 'No roles yet', 'Remove',
  'Each role receives only the permissions selected here.',
  'All role members automatically get access to the selected folders and their subfolders.',
  'Roles combine permissions and folder access for their members.', 'No roles available', 'New interface (hotplug)',
  'Cable disconnected, but the interface is still attached:', 'Automatic', '-1 shifts numbering: nic0 = MGMT, nic1 = ge0',
  'Interface is disabled or has no IP address', 'Interface is configured', 'Interface state is unknown', 'Interfaces and IP addresses…',
  'Memory page merging: identical pages of virtual machines share one physical copy, saving RAM.', 'Page merging is not available on this host',
  'Saved by page merging', 'Page merging (KSM/UKSM)', 'page merging',
  'KSM and UKSM solve the same task: enabling one turns the other off automatically.',
  'Standard kernel page merging: always available, uses some CPU.', 'Page merging with low CPU load; requires UKSM support in the kernel.',
  'Unavailable', 'On', 'Off', 'enabled', 'disabled', 'from role', 'Disk image', 'No image', 'Maximum simultaneously running laboratories',
  '0 means no limit. Only laboratories with machines running at the same time are counted.',
  'Choose the role of the account. Every role grants its own permissions and folder access.',
  'The administrator role always has every permission. Only the description can be changed.', 'Search by user, action, details or IP…', 'All users',
  'All actions', 'Period', 'All time', 'Last hour', 'Last 24 hours', 'Last 7 days', 'Last 30 days', 'Sort', 'Newest first', 'Oldest first',
  'Rows per page', 'Problems only', 'Shown', 'Next', 'Connection is not ready: an interface is disabled or has no IP address',
  'The terminal is open in another browser tab — switch to it', 'Confirm', 'Image file uploaded', 'Image catalog scanned',
  'Images added: {added}, updated: {updated}, problems: {problems}.', 'Image problem', 'Image deleted',
  'Image folder «{name}» removed from the catalog.', '{path} ({size}).', 'Folder access changed', 'Folders assigned: {count}. Initiator: «{user}».',
  'Machine failed to start', '{name}: QEMU exited with an error. Details are in the machine start log.', 'Machine crashed', 'Failed sign-in attempt',
  'user «{name}», address {ip}', 'Signed in', 'address {ip}', '«{name}»', 'User created', '{name} ({role})', 'Administrator changed your password',
  'Date and time: {time}. You need to sign in again.', 'User deleted', 'System settings changed', 'Memory page merging: {mechanism}', 'Enabled',
  'Your account changed', 'You changed your account', 'Changes: {changes_list}. {by_key}', 'Initiator: «{user}».',
  'The changes were made to your own account.', 'role: administrator', 'role: user', 'account disabled (active sessions ended)',
  'account active again', 'password (you need to sign in again)', 'Lab access changed',
  'Your permissions on «{lab}» changed. Changed permissions: {count}. {by_key}',
  'You changed your own permissions. Changed permissions: {count}. {by_key}', 'Laboratory created', 'Laboratory settings changed',
  'Laboratory deleted', '«{name}» was deleted by user «{user}». Machines stopped, data deleted without the possibility of recovery.',
  '«{name}»: machines reset — {done}, errors — {failed}.', 'Template created', 'Template copied', 'Template changed', 'Template deleted',
  'Template «{name}» in the catalog.', 'Machine added', '{name} → «{lab}»', 'Machines added', '{count} pcs.: {first} … {last} → «{lab}»',
  'Machine changed', '{name} in «{lab}»', 'Network created', 'Network changed', 'Cable created, but not fully applied', 'in «{lab}»',
  'Objects inserted', 'Objects deleted from «{lab}»', 'Machines: {devices}, networks: {networks}, cables: {cables}.', 'Machine started',
  'Machine force-stopped', 'Machine stopped', 'Machine restarted', 'Machine suspended', 'Machine resumed', 'Machine disk wiped',
  'Machine action: {action}', 'Image saved', '{device}: {mode}', 'Machine cloned', 'Copy of laboratory created',
  '«{name}» was copied to your copy: this is a separate laboratory.', 'Laboratory restored from a backup', '«{name}» added to the list.',
  'Laboratory backup created', '«{name}» exported to a file.', 'View laboratory and topology', 'Edit name and description', 'Save topology view',
  'Export and copy laboratory', 'Delete laboratory', 'Manage access for other users', 'Add machines', 'Edit machine parameters',
  'Move machines on canvas', 'Clone machines', 'Delete machines', 'Start machines', 'Stop and pause machines', 'Restart machines',
  'Wipe machine state', 'Saving images', 'Create cables', 'Delete cables', 'Manage machine network interfaces', 'Manage networks',
  'Attach disks and images', 'Detach disks', 'Serial console', 'VNC console', 'Copy and paste in the console', 'Virtual machines',
  'Additional capabilities', 'View laboratories', 'Create laboratories', 'Edit laboratories', 'Delete laboratories', 'Copy laboratories',
  'Start and stop machines', 'Wipe machines', 'Manage topology', 'View assigned folders', 'Access folder contents',
  'Move laboratories between folders', 'Manage folders', 'Start and stop', 'Console clipboard (copy and paste)', 'View users',
  'Change user settings', 'Change own password', 'Manage users', 'View notifications', 'Manage own settings', 'Access administrative sections',
  'NET', 'QEMU · KVM · NETWORK', 'Database error', 'Invalid id', 'Device not found', 'User not found', 'Icon not found', 'Invalid coordinates',
  'Template not found', 'The file is larger than 512 KB', 'Folder not found', 'Image not found', 'The device has no image selected',
  'PNG, JPEG, WEBP and SVG are supported', 'Password hashing error', 'No access to the target folder', 'Invalid connection in the file',
  'Could not save the folder', 'The theme catalog is not configured', 'The icon catalog is not configured', 'Instance not found',
  'Notification not found', 'A role with this name already exists', 'Role not found', 'Icon size: from 24 to 256',
  'Limit of simultaneously running laboratories: from 0 to 1000', 'Export error', 'Theme read error', 'Themes read error',
  'The objects do not belong to this laboratory', 'Object not found', 'No permission: creating laboratories', 'Invalid instance',
  'Invalid icon hash', 'Invalid user id', 'Unknown commit mode', 'Invalid Origin', 'Invalid role', 'Could not save the layout',
  'Could not save the icon', 'Could not read the file', 'The name is already taken', 'The name is invalid or already taken',
  'The name is taken by a folder or a laboratory at the root', 'The laboratory is running: stop it before renaming', 'The machine is running: stop it before renaming', 'The machine is running: stop it before changing its icon', 'Could not move the laboratory files', 'Could not move the machine files', 'The name is taken by a laboratory or a folder at the root',
  'Icon not found: upload the image again', 'Limit reached: 500 devices in a laboratory',
  'At least one active administrator must remain in the system', 'SVG: wrong namespace', 'A template with this name already exists',
  'VNC frame rate must be from 1 to 60', 'Commit is available only for a stopped machine', 'The file is too large or cannot be read',
  'The file is empty', 'The folders file is not specified', 'The device is connected to a running device — stop it or remove the cable first',
  'The device did not stop in time', 'Device not found in this laboratory', 'Cable management is not configured', 'Specify the read field',
  'Specify up: true or false', 'Specify mechanism: ksm or uksm', 'Specify enabled: true or false',
  'Remove the cables from the interfaces that will be deleted', 'The template must have a name',
  'The device has no image folder selected or it is empty (hda.qcow2 and/or ISO disks are needed)', 'The image must have a name and a folder',
  'The machine already has a working disk: reset the disk (Wipe) to change the image', 'The laboratory has no name', 'Authorization required',
  'Only an administrator can assign roles', 'Network type: isolated, nat or cloud', 'Network interface type: ens, te or ge',
  'The theme is too large', 'The current password is incorrect', 'Stop the running devices first', 'Upload your icon first',
  'Too many attempts, try again later', 'The machine id is too large for a network interface name',
  'The message is too large', 'The laboratory is too large', 'VNC compression must be from 1 to 9', 'Network not found in this laboratory',
  'Network not found', 'Network interfaces: from 1 to 32', 'The session is invalid', 'The server does not support upgrading to WebSocket',
  'Another operation is already running on this device', 'Empty file', 'Vendor: no more than 64 characters', 'A user with this name already exists',
  'Only WebSocket version 13 is supported', 'No more than 4 CD-ROMs per device are supported',
  'Only an administrator can overwrite the original image', 'Journal read error', 'Theme save error', 'Copy error', 'Journal write error',
  'Journal error', 'Stop the devices connected to the network', 'Stop the connected devices to change the network settings',
  'A WebSocket connection is expected', 'Connection: Upgrade is expected', 'Shared access: none or view',
  'The image is used by running devices — stop them to replace the files', 'The disk image can be changed only on a stopped device',
  'Nothing to paste', 'No permission: managing users', 'No permission to create themes', 'No machines to start in this instance', 'No changes',
  'No access to the parent folder', 'The device has no such interface', 'Unsupported disk type', 'Unsupported network card model',
  'Unsupported architecture', 'You cannot delete yourself', 'You cannot delete the last administrator', 'You cannot delete an administrator',
  'You cannot connect an object to itself', 'You cannot demote or disable yourself', 'You cannot move laboratories into a folder being deleted',
  'You cannot disable yourself', 'You cannot change your own permissions', 'You cannot change an administrator', 'Invalid QEMU machine type',
  'Invalid image path', 'Invalid Sec-WebSocket-Key', 'Invalid JSON', 'Invalid view data', 'Invalid parent folder', 'Invalid target folder',
  'Unknown file format (netrave-lab version 1 expected)', 'Unknown frame type', 'Unknown VNC encoding mode',
  'Not enough disk space to create the device disks', 'Not enough disk space for cloning', 'Not enough disk space for the upload',
  'Not enough space to overwrite the image', 'Not enough space in the image catalog to commit', 'Invalid template type', 'Invalid endpoint type',
  'Invalid characters in the QEMU parameters', 'Invalid VNC resolution', 'Invalid network interface name', 'Invalid template category',
  'Wrong username or password', 'Could not save the folders', 'Could not save the assignments', 'Could not create a session',
  'Could not create the image folder', 'Could not create a temporary file', 'Could not parse the qemu-img response',
  'Could not read the image catalog', 'Could not find free console ports', 'Could not stop the machine: the QEMU process did not exit',
  'Could not update the laboratories', 'Not an ISO 9660 image', 'No configuration file (-config): the settings were not saved',
  'Starting interface number: from -1 to 31 (-1 — shift: nic0 = MGMT, nic1 = ge0)', 'The NAT cloud bridge is not created',
  'aarch64 machines boot only through UEFI', 'Console: serial or vnc', 'Only a stopped device can be cloned', 'The client must mask frames',
  'VNC JPEG quality must be from 1 to 9', 'The laboratory directory is not set', 'The device interface is used in two connections',
  'The interface is already connected by a cable', 'The name is invalid', 'Icon: standard, custom or empty (automatic)', 'The record already exists',
  'Record not found', 'The upload was interrupted or exceeds the size limit', 'Limit reached: 32 network interfaces',
  'Additional QEMU parameters: no more than 512 characters', 'Working with images requires the «Images» permission',
  'UEFI needs firmware: OVMF (x86_64) or AAVMF (aarch64), see install.sh', 'No more than 6 disks per device are supported for SATA',
  'No more than 4 disks per device are supported for IDE; choose virtio or SATA',
  'The device disks are not created yet — start the device at least once', 'The disk refers to external files (backing/data-file) — not supported',
  'Network diagnostics are unavailable', 'The owner has all permissions', 'VNC bitrate must be from 1 to 100 Mbit/s', 'qemu-system-i386 not found',
  'qemu-img could not read the file (not a disk image or damaged)', 'qemu-img not found (install qemu-utils)',
  'ksm: page merging support not found in the kernel', 'ksm: unknown page merging mechanism',
  'dnsmasq not found (install the dnsmasq package for DHCP)', 'SVG: invalid root element', 'SVG: unfinished document',
  'SVG: invalid processing instruction', 'SVG: invalid directive', 'SVG: could not determine the size (width/height or viewBox needed)',
  'SVG: extra closing element', 'SVG: the root element must be <svg>', 'RAM: from 64 to 524288 MB',
  'QEMU did not confirm the interface removal (device_del)', 'QEMU did not respond over QMP within 15 seconds',
  'The NAT cloud is shared by all laboratories: the subnet, gateway and DHCP do not change', 'CPU: from 1 to 64', 'BIOS: seabios or uefi',
  'No permission: {permissions}', 'No permission: {permission_key}', 'Unknown setting: {name}', 'Invalid value for {name}',
  'Role limit reached: no more than {limit} laboratories can run at the same time. Stop another laboratory to start this one.',
  'Unknown permission: {permission_key}', 'You cannot grant or revoke a permission you do not have: {permission_key}',
  'File is over the {limit} MB limit (set in System settings)', 'Could not save the file: {detail}',
  'The image is used by devices ({count}) — replace it on them first', 'Folder limit reached ({limit})',
  'Folder nesting is too deep (maximum {max})', 'CSRF check failed', 'Available to administrators only', 'The device is not running',
  'The console port is not assigned', 'Could not save the folders of the role',
  'Only an administrator can change the role of a user',
  'Could not delete the laboratories of the user',
  'Allowed files: .qcow2, .raw, .img, .iso with a valid file name', 'The file is larger than {size} KB',
  'Damaged {format_key} file: {detail}', 'Size {width}×{height}: sides must be from {min} to {max} pixels', 'PNG re-encoding failed: {detail}',
  'The file is larger than {size} KB after processing', 'Library {owner}: {detail}', 'Damaged SVG: {detail}', 'SVG: damaged metadata: {detail}',
  'SVG: element <{element}> is not allowed', 'SVG: attribute {attribute} is not allowed', 'SVG: invalid id {id}',
  'SVG: invalid attribute value in {attribute}', 'Laboratory index: {detail}', 'Laboratory {id}: {detail}',
  'Laboratory {id} in the catalog is stored as {stored}', 'Device {id} is listed twice', 'Network {id} is listed twice',
  'Cable {id}: end not found in the laboratory', 'Cable {id} is listed twice', 'Port {port} of device {id} is already connected by a cable',
  'File could not be parsed as YAML: {detail}', 'The built-in role cannot be changed', 'Image folder «{name}»: {detail}',
  'Template «{name}»: {detail}', 'Unknown BIOS «{bios}»', 'Unknown console «{console}»', 'Image {id} not found',
  '{path}: invalid file name', '{path}: record name «{name}» does not match the file name',
  '{path}: number {id} is already taken by file {file}.yaml', 'The name «{name}» is already taken', 'Config {path}: the file root must be a mapping',
  'Could not connect to QEMU: {detail}', 'Database schema version {version} is not supported (version {required} is needed): recreate the database',
  'Schema creation: {detail}', 'Folders: {detail}', 'Icon library limit reached ({limit})',
  'Format «{format}» is not supported (qcow2 or raw is needed)', 'Device «{name}»: {detail}', 'Invalid device name «{name}»',
  'Invalid network type «{type}»', 'Invalid network name «{name}»', 'Invalid interface name «{name}»', 'Quantity: from 1 to {max}',
  'Cable: {detail}', 'Insert error: {detail}', 'Machine {id} not found',
  '{kind_key} «{name}»: 1–{max} characters, Latin letters, digits, underscore, hyphen and single spaces between words',
  'Username: 1–{max} characters (Latin letters, digits, underscore, hyphen and single spaces between words)',
  'Password: {min}–{max} characters (Latin letters, digits and symbols, no spaces)', 'The name «{name}» is taken by a built-in role',
  'Description was not saved: {detail}', 'User not found: {id}', '{file}: duplicate theme id «{id}»', 'Invalid theme id «{id}»',
  'Theme name «{id}»: from 1 to 40 characters', 'Theme «{id}»: unknown base «{base}»', 'Theme «{id}»: the full set of colors must be given',
  'Theme «{id}»: unknown color «{color}»', 'Theme «{id}»: invalid color «{color}»', 'Theme «{id}»: color «{color}» is missing',
  'Network {bridge}: {detail}', 'Cable {bridge}: {detail}', 'Enabling ip_forward: {detail}', 'Host interface «{host}» not found',
  'Interface {host} carries the default route: connecting it to the bridge would cut the host off the network; choose another interface or bridge',
  'Interface {host} is already in bridge {bridge}', 'NAT cloud isolation: {detail}', 'Machine {id}, interface {iface}: {detail}',
  'Interface {iface} of machine {id} was not created: restart the machine', 'Machine {id}, interface {iface}: hot unplug failed: {detail}',
  'QEMU parameter «{param}» is not allowed (allowed: -device -rtc -smbios -boot -global -vga -cpu -no-acpi -usb -no-hpet -k)',
  'QEMU parameter {param} has no value', 'Value «{value}» is not allowed for security reasons', 'Network of interface e{nic}: {detail}',
  'Creating image: {detail}', 'Moving disk: {detail}', 'Image backup: {detail}', 'Preparing disk: {detail}', 'Saving the previous image: {detail}',
  'Deferred disk switch of a dependent machine: {detail}', 'Checking the disk of a dependent machine: {detail}',
  'Switching the disk of a dependent machine: {detail}', 'Replacing the image: {detail}', 'Interface e{nic} is out of range',
  'QMP is unavailable: {detail}', 'No free PCIe slot ({slot}): restart the machine to add slots', 'QEMU not found: {path}',
  'Architecture {arch} does not run on this host ({host})',
  'Architecture {arch} does not run on this host ({host}): KVM runs only guests of its own architecture', 'KVM is unavailable: {detail}',
  'Disk file not found: {file}', 'ISO not found: {file}', 'Not enough free memory: {need} MB needed, {available} MB available',
  'Not enough disk space: {free} MB free, at least 2048 MB needed', 'Reading the deferred disk switch: {detail}',
  'Previous disk image not found: {file}', 'Reverting the disk to the previous image: {detail}', 'Removing the disk switch mark: {detail}',
  'The device is already running (state: {state})', 'Copying OVMF_VARS: {detail}', 'Could not start QEMU: {detail}',
  'Restart is not available in state {state}', 'The operation is not available in state {state}',
  'The machine is busy: wait for the operation to finish (state: {state})', 'Could not delete the working disks: {detail}', 'Create own themes',
  'Sections', 'Event log', 'Machine templates', 'Disk images', 'Cables and networks', 'Disks and consoles', '(bridge not created yet)',
  '(network not found)', '(no cable)', 'A folder cannot be moved into its own subfolder', 'A folder cannot be moved into itself',
  'Added {added}, updated {updated}', 'Backups', 'Bridge {name} does not exist', 'Cables',
  'Custom icon for {device} was not uploaded — chosen automatically', 'Database error: {detail}', 'Database read error: {detail}',
  'Description: no more than 2000 characters', 'Environment check failed', 'Folder {id} not found', 'Full access to the whole system',
  'Icon for {device} is damaged — chosen automatically', 'Icon for {device} is too large — chosen automatically',
  'Icon for {device} was not accepted: {detail} — chosen automatically', 'Icon for {device} was not saved — chosen automatically', 'Icon name',
  'Icon size of {device} is out of range — default size used', 'Icon {icon} for {device} is not supported — chosen automatically', 'Image folder',
  'Image name', 'Image {image} for {device} not found — choose it manually', 'Invalid colour set', 'Invalid colour: {name}', 'Invalid theme base',
  'Invalid theme identifier', 'Laboratory name', 'Laboratory opened', 'Laboratory {lab_id}, instance {instance}', 'Machine {id}: {detail}',
  'Machine {rid} ({device}), {interface}: TAP is missing',
  'Machine {rid} ({device}), {interface}: TAP {tap} is not in bridge {bridge} (now: {master})',
  'Machine {rid} ({device}), {interface}: cable is disconnected, but TAP {tap} is in bridge {bridge}', 'Machine «{name}»: {action}', 'Networks',
  'Parent folder not found', 'Role description: no more than 2000 characters', 'Sign-in and security', 'Template name', 'Too many custom themes',
  'Unknown theme', '{kind} {name} (laboratory of user «{owner}»)', '{lab}: instance {instance}, reset {devices}, errors {failed}',
  '{name}: folder deleted, but the image is used by machines', '{name}: folder {folder_id}',
  '{name}: invalid folder name', '{name}: no disks (.qcow2/.raw/.img) and no ISO — folder skipped',
  '{name}: no folder', '{name}: {detail}', '{name}: {detail_key}',
  '{what_key}: laboratory «{lab_name}» of user «{owner}» (id {lab_id})',
  '{what_key} (laboratory of user «{owner}», id {lab_id})',
  'B', 'KB', 'TB', 'd', 'h', 'm',
  '{kind_key} «{name}»: 1–{max} characters, no «/», control characters or leading dot',
  'Invalid folder name:',
  'Invalid file name:',
  'Send message', 'Message', 'Message text', 'Level', 'Information', 'Send', 'Sending…', 'Users will see the message in the notification centre.', 'Enter the message text', 'Message text: up to 2000 characters', 'Message level: info or warning', 'Choose the recipients', 'Too many recipients', 'No active recipients', 'Could not read the recipients', 'Message sent to {count} users', 'Message from {sender}', 'Messages from administrators',
  'Messages', 'Recipients', 'All active users', 'Recipients: {count}', 'Got it',
  'Device name', 'Network name', 'Device {name} is listed twice', 'Network {name} is listed twice', 'Cable {key}: end not found in the laboratory', 'Cable {key} is listed twice', 'Port {port} of device {name} is already connected by a cable', 'Invalid name', 'Invalid folder name', 'The folder is not empty', 'Target folder not found', 'A laboratory with this name already exists', 'Laboratory {name}: {detail}', '{name}: folder {folder}',
] as const;
type TranslationKey = (typeof EN_KEYS)[number];

/** Messages of the language, or an empty object when the language has no file. */
function messages(language: string): Partial<Record<TranslationKey, string>> {
  return locales[language] ?? {};
}

// Languages offered by the switcher. Each one needs `locales/<code>.yaml` and a label.
type Language = 'en' | 'ru';

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

/**
 * Translates a text that the server sent: an English key with {name} parameters.
 * Parameters ending in `_key` are themselves keys (translated with the same parameters);
 * parameters ending in `_list` are lists of keys separated by "; ".
 */
export function translateMessage(key: string, params?: Record<string, string>): string {
  const language = currentLanguage();
  let text = language === 'en' ? key : ((messages(language) as Record<string, string | undefined>)[key] ?? key);
  if (params) {
    // A nested key is translated with the plain parameters only: it must not see its own `_key` entry again.
    const plain = Object.fromEntries(Object.entries(params).filter(([name]) => !/_(key|list)$/.test(name)));
    for (const [name, value] of Object.entries(params)) {
      let resolved = value;
      if (name.endsWith('_key')) resolved = translateMessage(value, plain);
      else if (name.endsWith('_list')) resolved = value.split('; ').map((part) => translateMessage(part, plain)).join('; ');
      text = text.split(`{${name}}`).join(resolved);
    }
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
