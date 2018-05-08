import { ApiHelpers } from './components/util/ApiHelpers.jsx';
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

let controllerNs = appData.controllerNamespace || "conduit";

let api = ApiHelpers(pathPrefix);

let applicationHtml = (
  <BrowserRouter>
    <Layout>
      <Route
        render={routeProps => (<Sidebar
          {...routeProps}
          goVersion={appData.goVersion}
          releaseVersion={appData.releaseVersion}
          api={api}
          pathPrefix={pathPrefix}
          uuid={appData.uuid} />)} />
      <Layout>
        <Layout.Content style={{ margin: '0 0', padding: 0, background: '#fff' }}>
          <div className="main-content">
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/servicemesh`} />
              <Route path={`${pathPrefix}/servicemesh`} render={() => <ServiceMesh api={api} releaseVersion={appData.releaseVersion} controllerNamespace={controllerNs} />} />
              <Route path={`${pathPrefix}/namespaces/:namespace`} render={props => <Namespace resource="namespace" api={api} controllerNamespace={controllerNs} params={props.match.params} />} />
              <Route path={`${pathPrefix}/namespaces`} render={() => <ResourceList resource="namespace" api={api} controllerNamespace={controllerNs} />} />
              <Route path={`${pathPrefix}/deployments`} render={() => <ResourceList resource="deployment" api={api} controllerNamespace={controllerNs} />} />
              <Route path={`${pathPrefix}/replicationcontrollers`} render={() => <ResourceList resource="replication_controller" api={api} controllerNamespace={controllerNs} />} />
              <Route path={`${pathPrefix}/pods`} render={() => <ResourceList resource="pod" api={api} controllerNamespace={controllerNs} />} />
              <Route component={NoMatch} />
            </Switch>
          </div>
        </Layout.Content>
      </Layout>
    </Layout>
  </BrowserRouter>
);

ReactDOM.render(applicationHtml, appMain);
