import { useTranslation } from 'react-i18next';
import type { Locale } from 'date-fns';
import { dateFnsLocaleFor } from '../i18n/locales';

/** date-fns locale for the active UI language; pass as `{ locale }` to `format()`. */
export const useDateLocale = (): Locale => {
  const { i18n } = useTranslation();
  return dateFnsLocaleFor(i18n.language);
};

export default useDateLocale;
