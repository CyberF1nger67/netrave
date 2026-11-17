/**
  * Properties of the hidden `<input type="file">` that opens the folder picker.
 *
  * `webkitdirectory` is set directly in JSX, not in `useEffect`: setting the attribute
  * after mounting is unreliable - if the button is clicked before the effect runs
  * (or right after a page reload), the browser opens a file picker instead of
  * a folder picker.
 */
export const folderInputProps: Record<string, string> = { webkitdirectory: '', directory: '' };
