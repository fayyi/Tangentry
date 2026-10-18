export function shouldOpenToolbarPopoverOnFocus(focusVisible: boolean): boolean {
  return focusVisible
}

export function shouldCloseToolbarPopoverOnKey(key: string): boolean {
  return key === 'Escape'
}

export function shouldCloseToolbarPopoverForPointer(isInside: boolean): boolean {
  return !isInside
}
