import '../css/styles.css';
import '../img/favicon.png'; // needs to be referenced somewhere so webpack bundles it

import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import { DETECTORS, LocaleResolver, TRANSFORMERS } from 'locales-detector';
import { MuiThemeProvider, createMuiTheme } from '@material-ui/core/styles';

import ApiHelpers from './components/util/ApiHelpers.jsx';
import AppContext from './components/util/AppContext.jsx';
import Community from './components/Community.jsx';
import CssBaseline from '@material-ui/core/CssBaseline';
import Gateway from './components/Gateway.jsx';
import { I18nProvider } from '@lingui/react';
import Namespace from './components/Namespace.jsx';
import Navigation from './components/Navigation.jsx';
import NoMatch from './components/NoMatch.jsx';
import { QueryParamProvider } from 'use-query-params';
import React from 'react';
import ReactDOM from 'react-dom';
import ResourceDetail from './components/ResourceDetail.jsx';
import ResourceList from './components/ResourceList.jsx';
import ServiceMesh from './components/ServiceMesh.jsx';
import Tap from './components/Tap.jsx';
import Top from './components/Top.jsx';
import TopRoutes from './components/TopRoutes.jsx';
import _find from 'lodash/find';
import _isEmpty from 'lodash/isEmpty';
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
const catalogOptions = { en: catalogEn, es: catalogEs };
const selectedLocale =
    _find(detectedLocales, l => !_isEmpty(catalogOptions[l])) || 'en';
const selectedCatalog = catalogOptions[selectedLocale] || catalogEn;

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
        <I18nProvider
          language={selectedLocale}
          catalogs={{ [selectedLocale]: selectedCatalog }}>
          <AppHTML />
        </I18nProvider>
      </AppContext.Provider>
    );
  }
}

function AppHTML() {
  const theme = createMuiTheme(dashboardTheme);

  return (
    <React.Fragment>
      <CssBaseline />
      <MuiThemeProvider theme={theme}>
        <BrowserRouter>
          <QueryParamProvider ReactRouterRoute={Route}>
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/namespaces`} />
              <Redirect exact from={`${pathPrefix}/overview`} to={`${pathPrefix}/namespaces`} />
              <Redirect exact from={`${pathPrefix}/deployments`} to={`${pathPrefix}/namespaces/_all/deployments`} />
              <Redirect exact from={`${pathPrefix}/trafficsplits`} to={`${pathPrefix}/namespaces/_all/trafficsplits`} />
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
                path={`${pathPrefix}/namespaces/:namespace/trafficsplits/:trafficsplit`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/trafficsplits`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="trafficsplit" />} />
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
                path={`${pathPrefix}/namespaces/:namespace/deployments`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="deployment" />} />
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
              <Route component={NoMatch} />
            </Switch>
          </QueryParamProvider>
        </BrowserRouter>
      </MuiThemeProvider>
    </React.Fragment>
  );
}

ReactDOM.render(<App />, appMain);
