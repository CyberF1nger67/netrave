// The text field of the Serial terminal's clipboard panel. It is a native textarea: caret movement,
// selection, Home/End, Delete and editing in the middle of a line are left to the browser. What is
// set here is only what makes the field behave the same way across browsers.

/** Creates the clipboard textarea with settings that stop browser text rewriting (autocorrect, auto-capitalize). */
export function createClipField(placeholder: string, className: string): HTMLTextAreaElement {
  const field = document.createElement('textarea');
  field.className = className;
  field.placeholder = placeholder;
  field.spellcheck = false;
  // Non-standard attributes: Safari and iOS rewrite text in a textarea unless they are off, which moves the caret.
  field.setAttribute('autocomplete', 'off');
  field.setAttribute('autocorrect', 'off');
  field.setAttribute('autocapitalize', 'off');
  return field;
}

/**
 * Replaces the whole text of the field (used by Copy screen, Read clipboard and Clear). The caret is placed
 * at the end explicitly: browsers differ on where it lands after a programmatic value change.
 */
export function replaceClipText(field: HTMLTextAreaElement, text: string): void {
  field.value = text;
  field.setSelectionRange(text.length, text.length);
}
