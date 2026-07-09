diff --git a/chrome/browser/browseros/onboarding/browseros_onboarding_api.ts b/chrome/browser/browseros/onboarding/browseros_onboarding_api.ts
new file mode 100644
index 0000000000000..83a1cb5ea28c7
--- /dev/null
+++ b/chrome/browser/browseros/onboarding/browseros_onboarding_api.ts
@@ -0,0 +1,99 @@
+// Copyright 2026 The Chromium Authors
+// Use of this source code is governed by a BSD-style license that can be
+// found in the LICENSE file.
+
+export const BROWSEROS_ONBOARDING_API_VERSION = 1 as const;
+
+export type BrowserOSImportItem = 'history'|'bookmarks'|'cookies'|'passwords'|
+    'searchEngines'|'autofill'|'extensions';
+
+export type BrowserOSImportStatus =
+    'idle'|'detecting'|'ready'|'importing'|'succeeded'|'failed'|'completed';
+
+export const BrowserOSOnboardingMessage = {
+  PAGE_READY: 'browserosOnboardingPageReady',
+  REFRESH_SOURCES: 'browserosOnboardingRefreshSources',
+  START_IMPORT: 'browserosOnboardingStartImport',
+  COMPLETE: 'browserosOnboardingComplete',
+} as const;
+
+export type BrowserOSOnboardingMessage =
+    typeof BrowserOSOnboardingMessage[keyof typeof BrowserOSOnboardingMessage];
+
+export interface BrowserOSImportSource {
+  id: string;
+  displayName: string;
+  browserName: string;
+  profileName: string;
+  supportedItems: BrowserOSImportItem[];
+  recommendedItems: BrowserOSImportItem[];
+}
+
+export interface BrowserOSImportProgress {
+  currentItem?: BrowserOSImportItem;
+  currentSourceId?: string;
+  currentSourceName?: string;
+  completedItems: BrowserOSImportItem[];
+  totalItems: number;
+  completedSources?: number;
+  totalSources?: number;
+}
+
+export interface BrowserOSOnboardingError {
+  code: string;
+  message: string;
+}
+
+export interface BrowserOSImportSelection {
+  sourceId: string;
+  items?: BrowserOSImportItem[];
+}
+
+export type BrowserOSImportSourceResultStatus =
+    'pending'|'importing'|'succeeded'|'failed';
+
+export interface BrowserOSImportSourceResult {
+  sourceId: string;
+  displayName: string;
+  status: BrowserOSImportSourceResultStatus;
+}
+
+export interface BrowserOSOnboardingState {
+  apiVersion: typeof BROWSEROS_ONBOARDING_API_VERSION;
+  status: BrowserOSImportStatus;
+  sources: BrowserOSImportSource[];
+  progress?: BrowserOSImportProgress;
+  error?: BrowserOSOnboardingError;
+  /** Multi-source imports can end succeeded with failed per-source results. */
+  results?: BrowserOSImportSourceResult[];
+}
+
+/**
+ * Starts one source or an ordered multi-source import queue.
+ *
+ * Must be sent directly from the visible Import action. The browser process
+ * rejects hidden or non-interactive startImport messages because importing
+ * cookies/passwords can trigger the macOS Chrome Safe Storage keychain prompt.
+ */
+export interface BrowserOSStartImportRequest {
+  sourceId?: string;
+  items?: BrowserOSImportItem[];
+  /** When present, selections takes precedence over sourceId/items. */
+  selections?: BrowserOSImportSelection[];
+}
+
+export interface BrowserOSOnboardingClient {
+  receiveState(state: BrowserOSOnboardingState): void;
+}
+
+export interface BrowserOSOnboardingChrome {
+  send(message: BrowserOSOnboardingMessage, args?: unknown[]): void;
+}
+
+declare global {
+  interface Window {
+    browserosOnboarding?: BrowserOSOnboardingClient;
+  }
+
+  const chrome: BrowserOSOnboardingChrome;
+}
