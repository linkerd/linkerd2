import '../css/styles.css';
import '../img/favicon.png'; // needs to be referenced somewhere so webpack bundles it

import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import { DETECTORS, LocaleResolver, TRANSFORMERS } from 'locales-detector';
import { MuiThemeProvider, createTheme } from '@material-ui/core/styles';

import CssBaseline from '@material-ui/core/CssBaseline';
import { I18nProvider } from '@lingui/react';
import { i18n } from '@lingui/core';
import { en, es } from 'make-plural/plurals';
import { QueryParamProvider } from 'use-query-params';
import React from 'react';
import ReactDOM from 'react-dom';
import _find from 'lodash/find';
import _isEmpty from 'lodash/isEmpty';
import { ReactRouter5Adapter } from 'use-query-params/adapters/react-router-5';
import ApiHelpers from './components/util/ApiHelpers.jsx';
import AppContext from './components/util/AppContext.jsx';
import Community from './components/Community.jsx';
import Extensions from './components/Extensions.jsx';
import Gateway from './components/Gateway.jsx';
import Namespace from './components/Namespace.jsx';
import Navigation from './components/Navigation.jsx';
import NoMatch from './components/NoMatch.jsx';
import ResourceDetail from './components/ResourceDetail.jsx';
import ResourceList from './components/ResourceList.jsx';
import ServiceMesh from './components/ServiceMesh.jsx';
import Tap from './components/Tap.jsx';
import Top from './components/Top.jsx';
import TopRoutes from './components/TopRoutes.jsx';
import catalogEn from './locales/en/messages.js';
import catalogEs from './locales/es/messages.js';
import { dashboardTheme } from './components/util/theme.js';

const appMain = document.getElementById('main');
const appData = !appMain ? {} : appMain.dataset;

let pathPrefix = '';
const proxyPathMatch = window.location.pathname.match(/\/api\/v1\/namespaces\/.*\/proxy/g);
if (proxyPathMatch) {
  pathPrefix = proxyPathMatch[0];
}

let defaultNamespace = 'default';
const pathArray = window.location.pathname.split('/');

// if the current URL path specifies a namespace, this should become the
// defaultNamespace
if (pathArray[0] === '' && pathArray[1] === 'namespaces' && pathArray[2]) {
  defaultNamespace = pathArray[2];
// if the current URL path is a legacy path such as `/daemonsets`, the
// defaultNamespace should be "_all", unless the path is `/namespaces`
} else if (pathArray.length === 2 && pathArray[1] !== '' && pathArray[1] !== 'namespaces') {
  defaultNamespace = '_all';
}

const detectedLocales = new LocaleResolver(
  [new DETECTORS.NavigatorDetector()],
  [new TRANSFORMERS.FallbacksTransformer()],
).getLocales();
const langOptions = {
  en: {
    catalog: catalogEn,
    plurals: en,
  },
  es: {
    catalog: catalogEs,
    plurals: es,
  },
};
const selectedLocale =
    _find(detectedLocales, l => !_isEmpty(langOptions[l])) || 'en';
const selectedLangOptions = langOptions[selectedLocale] || langOptions.en;

i18n.loadLocaleData(selectedLocale, { plurals: selectedLangOptions.plurals });
i18n.load(selectedLocale, selectedLangOptions.catalog.messages);
i18n.activate(selectedLocale);

class App extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      ...appData,
    };

    this.state.api = ApiHelpers(pathPrefix);
    this.state.pathPrefix = pathPrefix;
    this.state.productName = 'Linkerd';
    this.state.selectedNamespace = defaultNamespace;

    this.state.updateNamespaceInContext = name => {
      this.setState({ selectedNamespace: name });
    };

    this.state.checkNamespaceMatch = path => {
      const { selectedNamespace } = this.state;
      const pathNamespace = path.split('/')[2];
      if (pathNamespace && pathNamespace !== selectedNamespace) {
        this.setState({ selectedNamespace: pathNamespace });
      }
    };
  }

  render() {
    return (
      <AppContext.Provider value={this.state}>
        <I18nProvider i18n={i18n}>
          <AppHTML />
        </I18nProvider>
      </AppContext.Provider>
    );
  }
}

const AppHTML = function() {
  const theme = createTheme(dashboardTheme);

  return (
    <React.Fragment>
      <CssBaseline />
      <MuiThemeProvider theme={theme}>
        <BrowserRouter>
          <QueryParamProvider adapter={ReactRouter6Adapter}>
            <Routes>
              <Route path={`${pathPrefix}/`} element={<Navigate replace to={`${pathPrefix}/namespaces`} />} />
              <Route path={`${pathPrefix}/overview`} element={<Navigate replace to={`${pathPrefix}/namespaces`} />} />
              <Route path={`${pathPrefix}/deployments`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/deployments`} />} />
              <Route path={`${pathPrefix}/services`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/services`} />} />
              <Route path={`${pathPrefix}/daemonsets`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/daemonsets`} />} />
              <Route path={`${pathPrefix}/statefulsets`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/statefulsets`} />} />
              <Route path={`${pathPrefix}/jobs`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/jobs`} />} />
              <Route path={`${pathPrefix}/replicationcontrollers`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/replicationcontrollers`} />} />
              <Route path={`${pathPrefix}/pods`} element={<Navigate replace to={`${pathPrefix}/namespaces/_all/pods`} />} />

              <Route
                path={`${pathPrefix}/controlplane`}
                element={<Navigation ChildComponent={ServiceMesh} />} />
              <Route
                path={`${pathPrefix}/gateways`}
                element={<Navigation ChildComponent={Gateway} resource="gateway" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace`}
                element={<Navigation ChildComponent={Namespace} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/pods/:pod`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/pods`}
                element={<Navigation ChildComponent={ResourceList} resource="pod" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/daemonsets/:daemonset`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/daemonsets`}
                element={<Navigation ChildComponent={ResourceList} resource="daemonset" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/statefulsets/:statefulset`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/statefulsets`}
                element={<Navigation ChildComponent={ResourceList} resource="statefulset" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/jobs/:job`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/jobs`}
                element={<Navigation ChildComponent={ResourceList} resource="job" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/deployments/:deployment`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/services/:service`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/deployments`}
                element={<Navigation ChildComponent={ResourceList} resource="deployment" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/services`}
                element={<Navigation ChildComponent={ResourceList} resource="service" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicationcontrollers/:replicationcontroller`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicationcontrollers`}
                element={<Navigation ChildComponent={ResourceList} resource="replicationcontroller" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/cronjobs/:cronjob`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/cronjobs`}
                element={<Navigation ChildComponent={ResourceList} resource="cronjob" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicasets/:replicaset`}
                element={<Navigation ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicasets`}
                element={<Navigation ChildComponent={ResourceList} resource="replicaset" />} />
              <Route
                path={`${pathPrefix}/tap`}
                element={<Navigation ChildComponent={Tap} />} />
              <Route
                path={`${pathPrefix}/top`}
                element={<Navigation ChildComponent={Top} />} />
              <Route
                path={`${pathPrefix}/routes`}
                element={<Navigation ChildComponent={TopRoutes} />} />
              <Route
                path={`${pathPrefix}/namespaces`}
                element={<Navigation ChildComponent={ResourceList} resource="namespace" />} />
              <Route
                path={`${pathPrefix}/community`}
                element={<Navigation ChildComponent={Community} />} />
              <Route
                path={`${pathPrefix}/extensions`}
                element={<Navigation ChildComponent={Extensions} />} />
              <Route path="*" element={<NoMatch />} />
            </Routes>
          </QueryParamProvider>
        </BrowserRouter>
      </MuiThemeProvider>
    </React.Fragment>
  );
};

ReactDOM.render(<App />, appMain);
