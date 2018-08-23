import ApiHelpers from './components/util/ApiHelpers.jsx';
import AppContext from './components/util/AppContext.jsx';
import BreadcrumbHeader from './components/BreadcrumbHeader.jsx';
import { Layout } from 'antd';
import Namespace from './components/Namespace.jsx';
import NamespaceLanding from './components/NamespaceLanding.jsx';
import NoMatch from './components/NoMatch.jsx';
import React from 'react';
import ReactDOM from 'react-dom';
import ResourceDetail from './components/ResourceDetail.jsx';
import ResourceList from './components/ResourceList.jsx';
import { RouterToUrlQuery } from 'react-url-query';
import ServiceMesh from './components/ServiceMesh.jsx';
import Sidebar from './components/Sidebar.jsx';
import Tap from './components/Tap.jsx';
import Top from './components/Top.jsx';
import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import './../css/styles.css';
import './../img/favicon.png'; // needs to be referenced somewhere so webpack bundles it

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

let applicationHtml = (
  <AppContext.Provider value={context}>
    <BrowserRouter>
      <RouterToUrlQuery>
        <Layout>
          <Route component={Sidebar} />
          <Layout>
            <Route component={BreadcrumbHeader}  />
            <Layout.Content style={{ margin: '64px 0', padding: 0, background: '#fff' }}>
              <div className="main-content">
                <Switch>
                  <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/overview`} />
                  <Route path={`${pathPrefix}/overview`} component={NamespaceLanding} />
                  <Route path={`${pathPrefix}/servicemesh`} component={ServiceMesh} />
                  <Route exact path={`${pathPrefix}/namespaces/:namespace`} component={Namespace} />
                  <Route path={`${pathPrefix}/namespaces/:namespace/pods/:pod`} component={ResourceDetail} />
                  <Route path={`${pathPrefix}/namespaces/:namespace/deployments/:deployment`} component={ResourceDetail} />
                  <Route path={`${pathPrefix}/namespaces/:namespace/replicationcontrollers/:replicationcontroller`} component={ResourceDetail} />
                  <Route path={`${pathPrefix}/tap`} component={Tap} />
                  <Route path={`${pathPrefix}/top`} component={Top} />
                  <Route
                    path={`${pathPrefix}/namespaces`}
                    render={() => <ResourceList resource="namespace" />} />
                  <Route
                    path={`${pathPrefix}/deployments`}
                    render={() => <ResourceList resource="deployment" />} />
                  <Route
                    path={`${pathPrefix}/replicationcontrollers`}
                    render={() => <ResourceList resource="replicationcontroller" />} />
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
      </RouterToUrlQuery>
    </BrowserRouter>
  </AppContext.Provider>
);

ReactDOM.render(applicationHtml, appMain);
