import '../css/styles.css';
import '../img/favicon.png'; // needs to be referenced somewhere so webpack bundles it

import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
// import { DETECTORS, LocaleResolver, TRANSFORMERS } from 'locales-detector';
import { MuiThemeProvider, createTheme } from '@material-ui/core/styles';

import CssBaseline from '@material-ui/core/CssBaseline';
import { I18nProvider } from '@lingui/react';
import { i18n } from '@lingui/core';
// import { en, es } from 'make-plural/plurals';
import { QueryParamProvider } from 'use-query-params';
import React from 'react';
import ReactDOM from 'react-dom';
// import _find from 'lodash/find';
// import _isEmpty from 'lodash/isEmpty';
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
import catalogEn from './locales/en/messages.json';
import catalogEs from './locales/es/messages.json';
import { dashboardTheme } from './components/util/theme.js';

import { Trans } from '@lingui/macro';

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

// const detectedLocales = new LocaleResolver(
//   [new DETECTORS.NavigatorDetector()],
//   [new TRANSFORMERS.FallbacksTransformer()],
// ).getLocales();
const langCatalogs = {
  en: catalogEn,
  es: catalogEs,
};

const locale = 'en';
// _find(detectedLocales, l => !_isEmpty(langOptions[l])) || 'en';
const langCatalog = langCatalogs[locale] || langCatalogs.en;
// eslint-disable-next-line no-console
console.log('Loaded catalog:', langCatalog);
i18n.load(locale, langCatalog);
i18n.activate(locale);
// eslint-disable-next-line no-console
console.log('Active locale:', i18n.locale);
// eslint-disable-next-line no-console
console.log(i18n._('columnTitleNoTraffic'));
i18n.debug = true;

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
          <Trans id="404Msg">404Msg</Trans>

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
          <QueryParamProvider adapter={ReactRouter5Adapter}>
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/namespaces`} />
              <Redirect exact from={`${pathPrefix}/overview`} to={`${pathPrefix}/namespaces`} />
              <Redirect exact from={`${pathPrefix}/deployments`} to={`${pathPrefix}/namespaces/_all/deployments`} />
              <Redirect exact from={`${pathPrefix}/services`} to={`${pathPrefix}/namespaces/_all/services`} />
              <Redirect exact from={`${pathPrefix}/daemonsets`} to={`${pathPrefix}/namespaces/_all/daemonsets`} />
              <Redirect exact from={`${pathPrefix}/statefulsets`} to={`${pathPrefix}/namespaces/_all/statefulsets`} />
              <Redirect exact from={`${pathPrefix}/jobs`} to={`${pathPrefix}/namespaces/_all/jobs`} />
              <Redirect exact from={`${pathPrefix}/replicationcontrollers`} to={`${pathPrefix}/namespaces/_all/replicationcontrollers`} />
              <Redirect exact from={`${pathPrefix}/pods`} to={`${pathPrefix}/namespaces/_all/pods`} />

              <Route
                path={`${pathPrefix}/controlplane`}
                render={props => <Navigation {...props} ChildComponent={ServiceMesh} />} />
              <Route
                path={`${pathPrefix}/gateways`}
                render={props => <Navigation {...props} ChildComponent={Gateway} resource="gateway" />} />
              <Route
                exact
                path={`${pathPrefix}/namespaces/:namespace`}
                render={props => <Navigation {...props} ChildComponent={Namespace} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/pods/:pod`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/pods`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="pod" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/daemonsets/:daemonset`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/daemonsets`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="daemonset" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/statefulsets/:statefulset`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/statefulsets`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="statefulset" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/jobs/:job`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/jobs`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="job" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/deployments/:deployment`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/services/:service`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/deployments`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="deployment" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/services`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="service" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicationcontrollers/:replicationcontroller`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicationcontrollers`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="replicationcontroller" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/cronjobs/:cronjob`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/cronjobs`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="cronjob" />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicasets/:replicaset`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicasets`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="replicaset" />} />
              <Route
                path={`${pathPrefix}/tap`}
                render={props => <Navigation {...props} ChildComponent={Tap} />} />
              <Route
                path={`${pathPrefix}/top`}
                render={props => <Navigation {...props} ChildComponent={Top} />} />
              <Route
                path={`${pathPrefix}/routes`}
                render={props => <Navigation {...props} ChildComponent={TopRoutes} />} />
              <Route
                path={`${pathPrefix}/namespaces`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="namespace" />} />
              <Route
                path={`${pathPrefix}/community`}
                render={props => <Navigation {...props} ChildComponent={Community} />} />
              <Route
                path={`${pathPrefix}/extensions`}
                render={props => <Navigation {...props} ChildComponent={Extensions} />} />
              <Route component={NoMatch} />
            </Switch>
          </QueryParamProvider>
        </BrowserRouter>
      </MuiThemeProvider>
    </React.Fragment>
  );
};

ReactDOM.render(<App />, appMain);
