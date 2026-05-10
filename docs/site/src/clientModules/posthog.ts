/**
 * PostHog tracking for the OpenUsage docs site.
 *
 * Build-time env vars `POSTHOG_KEY` and `POSTHOG_HOST` are plumbed
 * through `docusaurus.config.ts` → `customFields`. If `POSTHOG_KEY`
 * is unset, the module is a no-op.
 *
 * Captures `$pageview` on initial load and every Docusaurus SPA route
 * update. Autocapture is on; session recording and surveys are off.
 * The shared `openusage.analytics-consent` localStorage key (set by
 * the marketing-site consent banner) is honored — an explicit
 * `declined` value disables capture; any other value (including no
 * value) leaves capture enabled. Localhost and headless browsers are
 * skipped.
 */

import type {Location} from 'history';
import siteConfig from '@generated/docusaurus.config';

interface PostHogCustomFields {
  posthogKey?: string;
  posthogHost?: string;
}

const customFields = (siteConfig.customFields ?? {}) as PostHogCustomFields;
const POSTHOG_KEY = customFields.posthogKey?.trim() ?? '';
const POSTHOG_HOST =
  customFields.posthogHost?.trim() || 'https://eu.i.posthog.com';
const CONSENT_KEY = 'openusage.analytics-consent';

let initialized = false;
let ready = false;
let posthogModule: typeof import('posthog-js').default | null = null;

function envSupportsAnalytics(): boolean {
  if (typeof window === 'undefined' || !POSTHOG_KEY) {
    return false;
  }
  const host = window.location.hostname;
  if (
    host === 'localhost' ||
    host === '127.0.0.1' ||
    host === '[::1]' ||
    window.navigator.webdriver
  ) {
    return false;
  }
  return true;
}

function readConsent(): string | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.localStorage.getItem(CONSENT_KEY);
  } catch {
    return null;
  }
}

async function ensureInitialized(): Promise<boolean> {
  if (initialized) return ready;
  initialized = true;

  if (!envSupportsAnalytics()) {
    return false;
  }

  const mod = await import('posthog-js');
  posthogModule = mod.default;
  posthogModule.init(POSTHOG_KEY, {
    api_host: POSTHOG_HOST,
    autocapture: true,
    capture_pageleave: true,
    capture_pageview: false, // we trigger pageviews manually on route updates
    defaults: '2026-01-30',
    disable_session_recording: true,
    disable_surveys: true,
    opt_out_capturing_by_default: false,
  });

  if (readConsent() === 'declined') {
    posthogModule.opt_out_capturing();
  } else {
    posthogModule.opt_in_capturing();
  }

  ready = true;
  return true;
}

function capturePageview(location: Location, origin: 'load' | 'route'): void {
  if (!ready || !posthogModule) return;
  if (posthogModule.has_opted_out_capturing()) return;

  posthogModule.capture('$pageview', {
    origin,
    surface: 'docs',
    path: location.pathname,
    url: window.location.href,
  });
}

export function onRouteDidUpdate({
  location,
  previousLocation,
}: {
  location: Location;
  previousLocation: Location | null;
}): void {
  ensureInitialized().then((ok) => {
    if (!ok) return;
    capturePageview(location, previousLocation ? 'route' : 'load');
  });
}
