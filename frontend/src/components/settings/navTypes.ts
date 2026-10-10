import React from 'react';
import type { StatusKey } from '../../services/settingsStatus';

export type SaveMode = 'autosave' | 'apply' | 'mixed';

export interface SettingsSection {
  id: string;
  /** URL segment under the group. */
  path: string;
  /** i18n key (namespace given by the shell) for the label. */
  labelKey: string;
  /** i18n key for the one-line description under the section title. */
  descKey?: string;
  Component: React.LazyExoticComponent<React.ComponentType<any>> | React.ComponentType<any>;
  /** Status probes that drive this section's attention badge. */
  statusKeys?: StatusKey[];
  saveMode: SaveMode;
  /** Section has nested routes (render with a trailing splat). */
  nested?: boolean;
}

export interface SettingsGroup {
  id: string;
  path: string;
  labelKey: string;
  icon: React.ReactNode;
  sections: SettingsSection[];
}

/** URL for a section; a group with an empty `path` sits directly under the base. */
export const sectionPath = (basePath: string, group: Pick<SettingsGroup, 'path'>, section: Pick<SettingsSection, 'path'>): string =>
  group.path ? `${basePath}/${group.path}/${section.path}` : `${basePath}/${section.path}`;
