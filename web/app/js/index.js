import { ApiHelpers } from './components/util/ApiHelpers.jsx';
import DeploymentsList from './components/DeploymentsList.jsx';
import { Layout } from 'antd';
import NoMatch from './components/NoMatch.jsx';
import React from 'react';
import ReactDOM from 'react-dom';
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

let api = ApiHelpers(pathPrefix);

let applicationHtml = hideSidebar => (
  <BrowserRouter>
    <Layout>
      <Layout.Sider
        width="310"
        breakpoint="lg"
        collapsible={true}
        collapsedWidth={0}
        onCollapse={onSidebarCollapse}>
        <Route
          render={routeProps => (<Sidebar
            {...routeProps}
            goVersion={appData.goVersion}
            releaseVersion={appData.releaseVersion}
            api={api}
            collapsed={hideSidebar}
            pathPrefix={pathPrefix}
            uuid={appData.uuid} />)} />
      </Layout.Sider>
      <Layout>
        <Layout.Content style={{ margin: '0 0', padding: 0, background: '#fff' }}>
          <div className="main-content">
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/servicemesh`} />
              <Route path={`${pathPrefix}/servicemesh`} render={() => <ServiceMesh api={api} releaseVersion={appData.releaseVersion} controllerNamespace={appData.controllerNamespace} />} />
              <Route path={`${pathPrefix}/deployments`} render={() => <DeploymentsList api={api} />} />
              <Route component={NoMatch} />
            </Switch>
          </div>
        </Layout.Content>
      </Layout>
    </Layout>
  </BrowserRouter>
);

const onSidebarCollapse = isHidden => {
  ReactDOM.render(applicationHtml(isHidden), appMain);
};

ReactDOM.render(applicationHtml(false), appMain);
