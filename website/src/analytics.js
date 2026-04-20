import posthog from "posthog-js";

const analyticsKey = import.meta.env.VITE_POSTHOG_KEY?.trim();
const analyticsHost = import.meta.env.VITE_POSTHOG_HOST?.trim() || "https://eu.i.posthog.com";
const consentStorageKey = "openusage.analytics-consent";

let analyticsReady = false;
let analyticsInitialized = false;

function canInitializeAnalytics() {
  if (typeof window === "undefined" || !analyticsKey) {
    return false;
  }

  const host = window.location.hostname;
  if (host === "localhost" || host === "127.0.0.1" || host === "[::1]" || window.navigator.webdriver) {
    return false;
  }

  return true;
}

function readConsentChoice() {
  if (typeof window === "undefined") {
    return null;
  }

  try {
    return window.localStorage.getItem(consentStorageKey);
  } catch {
    return null;
  }
}

function writeConsentChoice(value) {
  if (typeof window === "undefined") {
    return;
  }

  try {
    window.localStorage.setItem(consentStorageKey, value);
  } catch {
    // Ignore storage failures and keep the app usable.
  }
}

function capturePageview(origin) {
  if (!analyticsReady || posthog.has_opted_out_capturing()) {
    return;
  }

  posthog.capture("$pageview", {
    origin,
    path: window.location.pathname,
    url: window.location.href,
  });
}

export function initAnalytics() {
  if (analyticsInitialized) {
    return analyticsReady;
  }

  analyticsInitialized = true;
  analyticsReady = canInitializeAnalytics();
  if (!analyticsReady) {
    return false;
  }

  posthog.init(analyticsKey, {
    api_host: analyticsHost,
    autocapture: false,
    capture_pageleave: false,
    capture_pageview: false,
    defaults: "2026-01-30",
    disable_session_recording: true,
    disable_surveys: true,
    opt_out_capturing_by_default: true,
  });

  if (readConsentChoice() === "accepted") {
    posthog.opt_in_capturing();
    capturePageview("load");
  } else {
    posthog.opt_out_capturing();
  }

  return true;
}

export function analyticsConfigured() {
  return canInitializeAnalytics();
}

export function hasConsentChoice() {
  return readConsentChoice() !== null;
}

export function analyticsConsentChoice() {
  return readConsentChoice();
}

export function acceptAnalytics() {
  writeConsentChoice("accepted");
  if (!analyticsReady) {
    return;
  }

  posthog.opt_in_capturing();
  posthog.capture("analytics consent accepted");
  capturePageview("consent");
}

export function declineAnalytics() {
  writeConsentChoice("declined");
  if (!analyticsReady) {
    return;
  }

  posthog.opt_out_capturing();
}

export function track(event, properties = {}) {
  if (!analyticsReady || posthog.has_opted_out_capturing()) {
    return;
  }

  posthog.capture(event, properties);
}
