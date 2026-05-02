// Hoopoe-owned. Public re-exports for the Settings screen (hp-wg5p).

export { SettingsScreen, type SettingsScreenProps } from "./SettingsScreen.tsx";
export { SettingRow, type SettingRowProps } from "./SettingRow.tsx";
export {
  SETTING_DESCRIPTORS,
  SECTION_LABELS,
  SECTION_ORDER,
  groupBySections,
  resolveSettingSource,
  type SettingDescriptor,
  type SettingSection,
  type SettingSourceTier,
  type SettingWidgetKind,
  type SourceResolution,
} from "./SettingsModel.ts";
export {
  searchSettings,
  dimmedDescriptors,
  type SettingsSearchHit,
} from "./SettingsSearch.ts";
