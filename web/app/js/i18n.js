import en from './locale/en.json';
import es from './locale/es.json';
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

i18n
  .use(initReactI18next)
  .init({
    resources: { en, es },
    lng: navigator.language.split("-")[0],
    fallbackLng: "en",
    defaultNS: "common",
    keySeparator: false,
    interpolation: {
      escapeValue: false,
    },
    nsSeparator: "::",
    useSuspense: false,
    react: {
      wait: false,
      bindI18n: "languageChanged loaded",
      nsMode: "fallback",
    }
  });

export default i18n;
