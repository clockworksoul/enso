// Memory Ensō API module exposes the plugin public contract (extensions
// boundary rule: production code imports from the published `openclaw`
// package's plugin-sdk subpaths only, never from monorepo-internal paths).
export { definePluginEntry, type OpenClawPluginApi } from "openclaw/plugin-sdk/plugin-entry";
