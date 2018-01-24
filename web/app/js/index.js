import DeploymentDetail from './components/DeploymentDetail.jsx';
import DeploymentsList from './components/DeploymentsList.jsx';
import { Layout } from 'antd';
import NoMatch from './components/NoMatch.jsx';
import Paths from './components/Paths.jsx';
import PodDetail from './components/PodDetail.jsx';
import React from 'react';
import ReactDOM from 'react-dom';
import Routes from './components/Routes.jsx';
import ServiceMesh from './components/ServiceMesh.jsx';
import Sidebar from './components/Sidebar.jsx';
import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import './../css/styles.css';

let appMain = document.getElementById('main');
let appData = appMain.dataset;

let pathPrefix = "";
let proxyPathMatch = window.location.pathname.match(/\/api\/v1\/namespaces\/.*\/proxy/g);
if (proxyPathMatch) {
  pathPrefix = proxyPathMatch[0];
}

ReactDOM.render((
  <BrowserRouter>
    <Layout>
      <Layout.Sider width="310">
        <Route render={routeProps => <Sidebar {...routeProps} goVersion={appData.goVersion} releaseVersion={appData.releaseVersion} pathPrefix={pathPrefix} uuid={appData.uuid} />} />
      </Layout.Sider>
      <Layout>
        <Layout.Content style={{ margin: '0 0', padding: 0, background: '#fff' }}>
          <div className="main-content">
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/servicemesh`} />
              <Route path={`${pathPrefix}/servicemesh`} render={() => <ServiceMesh pathPrefix={pathPrefix} releaseVersion={appData.releaseVersion} />} />
              <Route path={`${pathPrefix}/deployments`} render={() => <DeploymentsList pathPrefix={pathPrefix} />} />
              <Route path={`${pathPrefix}/deployment`} render={props => <DeploymentDetail pathPrefix={pathPrefix} location={props.location} />} />
              <Route path={`${pathPrefix}/paths`} render={props => <Paths pathPrefix={pathPrefix} location={props.location} />} />
              <Route path={`${pathPrefix}/pod`} render={props => <PodDetail pathPrefix={pathPrefix} location={props.location} />} />
              <Route path={`${pathPrefix}/routes`} render={() => <Routes pathPrefix={pathPrefix} />} />
              <Route component={NoMatch} />
            </Switch>
          </div>
        </Layout.Content>
      </Layout>
    </Layout>
  </BrowserRouter>
), appMain);
