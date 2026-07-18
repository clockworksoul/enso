// Memory Ensō API module exposes the plugin public contract (extensions
// boundary rule: production code imports from openclaw/plugin-sdk/* via this
// local barrel only).
export { definePluginEntry, type OpenClawPluginApi } from "openclaw/plugin-sdk/plugin-entry";
