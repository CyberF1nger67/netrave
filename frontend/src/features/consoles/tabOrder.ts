/**
 * Moves the entry with id `from` to the position of the entry with id `to`.
 * Dragging to the right puts the item after the target, dragging to the left puts it before, so the
 * item lands where the pointer is. Returns a new list; unknown ids leave the order unchanged.
 */
export function moveEntry<T extends { id: number }>(list: readonly T[], from: number, to: number): T[] {
  const fromIndex = list.findIndex((item) => item.id === from);
  const toIndex = list.findIndex((item) => item.id === to);
  if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return [...list];
  const next = [...list];
  const [moved] = next.splice(fromIndex, 1);
  if (!moved) return [...list];
  next.splice(toIndex, 0, moved);
  return next;
}
