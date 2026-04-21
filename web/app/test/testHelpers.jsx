import _merge from 'lodash/merge';
import ApiHelpers from '../js/components/util/ApiHelpers.jsx';
import { createMemoryHistory } from 'history';
import React from 'react';
import { Route, Router } from 'react-router-dom';
import { I18nProvider } from '@lingui/react';
import { i18n } from '@lingui/core';
import { en } from 'make-plural/plurals'
import catalogEn from './../js/locales/en/messages.js';

const componentDefaultProps = {
  api: ApiHelpers(''),
  controllerNamespace: 'shiny-controller-ns',
  productName: 'ShinyProductName',
  releaseVersion: ''
};

i18n.loadLocaleData('en', { plurals: en })
i18n.loadLocaleData('en');
i18n.load('en', catalogEn.messages);
i18n.activate('en');

export function routerWrap(Component, extraProps={}, route="/", currentLoc="/") {
  const createElement = (ComponentToWrap, props) => (
    <ComponentToWrap {...(_merge({}, componentDefaultProps, props, extraProps))} />
  );
  return (
    <Router history={createMemoryHistory(currentLoc)} createElement={createElement}>
      <Route path={route} render={props => createElement(Component, props)} />
    </Router>
  );
}

export function i18nWrap(Component) {
  return (
    <I18nProvider i18n={i18n}>
      {Component}
    </I18nProvider>
  );
}

export function i18nAndRouterWrap(component, props) { return i18nWrap(routerWrap(component, props))};
