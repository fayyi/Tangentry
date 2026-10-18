export function shouldOpenToolbarPopoverOnFocus(focusVisible: boolean): boolean {
  return focusVisible
}

export type ToolbarPopoverCloseReason = 'escape' | 'outside' | 'blur' | 'action' | 'explicit'

export function toolbarPopoverCloseBehavior(reason: ToolbarPopoverCloseReason): { restoreFocus: boolean; suppressFocusOpen: boolean } {
  const restoreFocus = reason === 'escape' || reason === 'explicit'
  return { restoreFocus, suppressFocusOpen: restoreFocus }
}

export function shouldCloseToolbarPopoverOnKey(key: string): boolean {
  return key === 'Escape'
}

export function shouldCloseToolbarPopoverForPointer(isInside: boolean): boolean {
  return !isInside
}
