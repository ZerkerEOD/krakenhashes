import { csCZ, deDE, enUS, esES, frFR, itIT, jaJP, koKR, nlNL, plPL, ptBR, ruRU, trTR, ukUA, viVN, zhCN } from '@mui/x-data-grid/locales';
import type { GridLocaleText } from '@mui/x-data-grid';

const LOCALES: Record<string, Partial<GridLocaleText>> = {
  en: enUS.components.MuiDataGrid.defaultProps.localeText,
  de: deDE.components.MuiDataGrid.defaultProps.localeText,
  es: esES.components.MuiDataGrid.defaultProps.localeText,
  fr: frFR.components.MuiDataGrid.defaultProps.localeText,
  nl: nlNL.components.MuiDataGrid.defaultProps.localeText,
  ru: ruRU.components.MuiDataGrid.defaultProps.localeText,
  zh: zhCN.components.MuiDataGrid.defaultProps.localeText,
  ja: jaJP.components.MuiDataGrid.defaultProps.localeText,
  ko: koKR.components.MuiDataGrid.defaultProps.localeText,
  'pt-br': ptBR.components.MuiDataGrid.defaultProps.localeText,
  pt: ptBR.components.MuiDataGrid.defaultProps.localeText,
  it: itIT.components.MuiDataGrid.defaultProps.localeText,
  pl: plPL.components.MuiDataGrid.defaultProps.localeText,
  tr: trTR.components.MuiDataGrid.defaultProps.localeText,
  uk: ukUA.components.MuiDataGrid.defaultProps.localeText,
  cs: csCZ.components.MuiDataGrid.defaultProps.localeText,
  vi: viVN.components.MuiDataGrid.defaultProps.localeText,
  // No Indonesian DataGrid locale upstream: grid chrome stays English.
};

/** DataGrid footer/overlay strings for the active i18n language. */
export const gridLocaleText = (language: string | undefined): Partial<GridLocaleText> => {
  const full = (language ?? 'en').toLowerCase();
  return LOCALES[full] ?? LOCALES[full.split('-')[0]] ?? LOCALES.en;
};

export default gridLocaleText;
