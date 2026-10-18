/** Poll only while the discovery Activity is visible and within its retry budget. */
export function canPollPlatformVerification(active: boolean, attempts: number, maxAttempts = 120) {
  return active && attempts < maxAttempts
}
