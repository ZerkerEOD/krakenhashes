/**
 * Third-party locale data keyed by our i18n language codes: MUI core component
 * text (pagination, autocomplete, …) and date-fns (month/day names).
 */
import {
  csCZ, deDE, enUS, esES, frFR, idID, itIT, jaJP, koKR, nlNL, plPL, ptBR, ruRU, trTR, ukUA, viVN, zhCN,
} from '@mui/material/locale';
import type { Localization } from '@mui/material/locale';
import type { Locale } from 'date-fns';
import { cs, de, enUS as dfEnUS, es, fr, id, it, ja, ko, nl, pl, ptBR as dfPtBR, ru, tr, uk, vi, zhCN as dfZhCN } from 'date-fns/locale';

const MUI: Record<string, Localization> = {
  en: enUS, zh: zhCN, de: deDE, nl: nlNL, es: esES, ru: ruRU, fr: frFR, ja: jaJP, ko: koKR,
  'pt-br': ptBR, pt: ptBR, it: itIT, pl: plPL, tr: trTR, uk: ukUA, cs: csCZ, vi: viVN, id: idID,
};

const DATE_FNS: Record<string, Locale> = {
  en: dfEnUS, zh: dfZhCN, de, nl, es, ru, fr, ja, ko,
  'pt-br': dfPtBR, pt: dfPtBR, it, pl, tr, uk, cs, vi, id,
};

const pick = <T,>(table: Record<string, T>, language: string | undefined, fallback: T): T => {
  const full = (language ?? 'en').toLowerCase();
  return table[full] ?? table[full.split('-')[0]] ?? fallback;
};

export const muiLocaleFor = (language: string | undefined): Localization => pick(MUI, language, enUS);
export const dateFnsLocaleFor = (language: string | undefined): Locale => pick(DATE_FNS, language, dfEnUS);
