import _ from 'lodash';
import ApiHelpers from './components/util/ApiHelpers.jsx';
import AppContext from './components/util/AppContext.jsx';
import { Layout } from 'antd';
import Namespace from './components/Namespace.jsx';
import NoMatch from './components/NoMatch.jsx';
import React from 'react';
import ReactDOM from 'react-dom';
import ResourceList from './components/ResourceList.jsx';
import ServiceMesh from './components/ServiceMesh.jsx';
import Sidebar from './components/Sidebar.jsx';
import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import './../css/styles.css';

let appMain = document.getElementById('main');
let appData = !appMain ? {} : appMain.dataset;

let pathPrefix = "";
let proxyPathMatch = window.location.pathname.match(/\/api\/v1\/namespaces\/.*\/proxy/g);
if (proxyPathMatch) {
  pathPrefix = proxyPathMatch[0];
}

if (_.isEmpty(appData.controllerNamespace)) {
  appData.controllerNamespace = 'conduit';
}

const context = {
  ...appData,
  api: ApiHelpers(pathPrefix),
  pathPrefix: pathPrefix,
};

let applicationHtml = (
  <AppContext.Provider value={context}>
    <BrowserRouter>
      <Layout>
        <Route component={Sidebar} />
        <Layout>
          <Layout.Content style={{ margin: '0 0', padding: 0, background: '#fff' }}>
            <div className="main-content">
              <Switch>
                <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/servicemesh`} />
                <Route path={`${pathPrefix}/servicemesh`} component={ServiceMesh} />
                <Route path={`${pathPrefix}/namespaces/:namespace`} component={Namespace} />
                <Route
                  path={`${pathPrefix}/namespaces`}
                  render={() => <ResourceList resource="namespace" />} />
                <Route
                  path={`${pathPrefix}/deployments`}
                  render={() => <ResourceList resource="deployment" />} />
                <Route
                  path={`${pathPrefix}/replicationcontrollers`}
                  render={() => <ResourceList resource="replication_controller" />} />
                <Route
                  path={`${pathPrefix}/pods`}
                  render={() => <ResourceList resource="pod" />} />
                <Route
                  path={`${pathPrefix}/authorities`}
                  render={() => <ResourceList resource="authority" />} />
                <Route component={NoMatch} />
              </Switch>
            </div>
          </Layout.Content>
        </Layout>
      </Layout>
    </BrowserRouter>
  </AppContext.Provider>
);

ReactDOM.render(applicationHtml, appMain);
