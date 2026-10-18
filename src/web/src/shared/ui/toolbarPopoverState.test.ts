import { expect, test } from 'vitest'
import { shouldCloseToolbarPopoverForPointer, shouldCloseToolbarPopoverOnKey, shouldOpenToolbarPopoverOnFocus } from './toolbarPopoverState'

test('toolbar popover opens only for a visible keyboard focus', () => {
  expect(shouldOpenToolbarPopoverOnFocus(true)).toBe(true)
  expect(shouldOpenToolbarPopoverOnFocus(false)).toBe(false)
})

test('toolbar popover closes for Escape and outside pointers', () => {
  expect(shouldCloseToolbarPopoverOnKey('Escape')).toBe(true)
  expect(shouldCloseToolbarPopoverOnKey('Enter')).toBe(false)
  expect(shouldCloseToolbarPopoverForPointer(false)).toBe(true)
  expect(shouldCloseToolbarPopoverForPointer(true)).toBe(false)
})
