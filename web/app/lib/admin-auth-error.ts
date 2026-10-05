import { adminLoginHref } from '~/lib/admin'

export interface AdminAuthErrorPresentation {
  code: string
  title: string
  message: string
  retry: boolean
  loginHref: string
}

const adminAuthErrorCopy: Record<string, { title: string, message: string, retry: boolean }> = {
  authentication_rejected: {
    title: "Sign-in wasn't completed",
    message: 'The identity provider did not finish signing you in, or the sign-in could not be verified. You can try signing in again.',
    retry: true,
  },
  authentication_unavailable: {
    title: 'Sign-in is temporarily unavailable',
    message: 'The identity provider could not be reached. Try again shortly, or contact your Marketplace operator if it continues.',
    retry: true,
  },
  authentication_request_invalid: {
    title: "Sign-in request wasn't accepted",
    message: 'The sign-in request or the identity provider response was invalid. Start sign-in again from the Marketplace.',
    retry: true,
  },
  forbidden: {
    title: 'Administration access required',
    message: 'You signed in successfully, but your account does not have Marketplace administration permission. Contact your operator to request access.',
    retry: false,
  },
  admin_unavailable: {
    title: 'Administration sign-in is not configured',
    message: 'Marketplace administration sign-in is unavailable right now. An operator must repair the authentication configuration before you can sign in.',
    retry: false,
  },
  origin_rejected: {
    title: 'Sign-in must start from the Marketplace',
    message: 'This sign-in request did not come from the configured Marketplace address. Open the Marketplace directly and sign in from there.',
    retry: false,
  },
}

const genericAdminAuthError = {
  code: 'sign_in_failed',
  title: "Sign-in couldn't be completed",
  message: 'Something prevented sign-in from finishing. You can try signing in again from the Marketplace.',
  retry: true,
}

export function resolveAdminAuthError(query: Record<string, unknown>, baseURL = '/marketplace/'): AdminAuthErrorPresentation {
  const rawCode = query.code
  const known = typeof rawCode === 'string' && Object.prototype.hasOwnProperty.call(adminAuthErrorCopy, rawCode)
  const copy = known ? adminAuthErrorCopy[rawCode as string]! : genericAdminAuthError
  const rawNext = query.next
  const next = typeof rawNext === 'string' ? rawNext : ''
  return {
    code: known ? rawCode as string : genericAdminAuthError.code,
    title: copy.title,
    message: copy.message,
    retry: copy.retry,
    loginHref: adminLoginHref(next, baseURL),
  }
}
