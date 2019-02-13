import './../css/styles.css';
import './../img/favicon.png'; // needs to be referenced somewhere so webpack bundles it

import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import { MuiThemeProvider, createMuiTheme } from '@material-ui/core/styles';

import ApiHelpers from './components/util/ApiHelpers.jsx';
import AppContext from './components/util/AppContext.jsx';
import CssBaseline from '@material-ui/core/CssBaseline';
import Endpoints from './components/Endpoints.jsx';
import Namespace from './components/Namespace.jsx';
import NamespaceLanding from './components/NamespaceLanding.jsx';
import Navigation from './components/Navigation.jsx';
import NoMatch from './components/NoMatch.jsx';
import React from 'react';
import ReactDOM from 'react-dom';
import ResourceDetail from './components/ResourceDetail.jsx';
import ResourceList from './components/ResourceList.jsx';
import { RouterToUrlQuery } from 'react-url-query';
import ServiceMesh from './components/ServiceMesh.jsx';
import Tap from './components/Tap.jsx';
import Top from './components/Top.jsx';
import TopRoutes from './components/TopRoutes.jsx';
import { dashboardTheme } from './components/util/theme.js';

let appMain = document.getElementById('main');
let appData = !appMain ? {} : appMain.dataset;

let pathPrefix = "";
let proxyPathMatch = window.location.pathname.match(/\/api\/v1\/namespaces\/.*\/proxy/g);
if (proxyPathMatch) {
  pathPrefix = proxyPathMatch[0];
}

const context = {
  ...appData,
  api: ApiHelpers(pathPrefix),
  pathPrefix: pathPrefix,
  productName: "Linkerd"
};

const theme = createMuiTheme(dashboardTheme);

let applicationHtml = (
  <React.Fragment>
    <CssBaseline />
    <MuiThemeProvider theme={theme}>
      <AppContext.Provider value={context}>
        <BrowserRouter>
          <RouterToUrlQuery>
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/overview`} />
              <Route
                path={`${pathPrefix}/overview`}
                render={props => <Navigation {...props} ChildComponent={NamespaceLanding} />} />
              <Route
                path={`${pathPrefix}/servicemesh`}
                render={props => <Navigation {...props} ChildComponent={ServiceMesh} />} />
              <Route
                exact
                path={`${pathPrefix}/namespaces/:namespace`}
                render={props => <Navigation {...props} ChildComponent={Namespace} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/pods/:pod`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/daemonsets/:daemonset`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/statefulsets/:statefulset`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/deployments/:deployment`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
              <Route
                path={`${pathPrefix}/namespaces/:namespace/replicationcontrollers/:replicationcontroller`}
                render={props => <Navigation {...props} ChildComponent={ResourceDetail} />} />
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
                path={`${pathPrefix}/deployments`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="deployment" />} />
              <Route
                path={`${pathPrefix}/daemonsets`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="daemonset" />} />
              <Route
                path={`${pathPrefix}/statefulsets`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="statefulset" />} />
              <Route
                path={`${pathPrefix}/replicationcontrollers`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="replicationcontroller" />} />
              <Route
                path={`${pathPrefix}/pods`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="pod" />} />
              <Route
                path={`${pathPrefix}/authorities`}
                render={props => <Navigation {...props} ChildComponent={ResourceList} resource="authority" />} />
              <Route
                path={`${pathPrefix}/endpoints`}
                render={props => <Navigation {...props} ChildComponent={Endpoints} />} />
              <Route component={NoMatch} />
            </Switch>
          </RouterToUrlQuery>
        </BrowserRouter>
      </AppContext.Provider>
    </MuiThemeProvider>
  </React.Fragment>
);

ReactDOM.render(applicationHtml, appMain);
