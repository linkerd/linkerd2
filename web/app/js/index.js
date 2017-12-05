import _ from 'lodash';
import React from 'react';
import ReactDOM from 'react-dom';
import NoMatch from './components/NoMatch.jsx';
import Sidebar from './components/Sidebar.jsx';
import ServiceMesh from './components/ServiceMesh.jsx';
import Deployments from './components/Deployments.jsx';
import Deployment from './components/Deployment.jsx';
import PodDetail from './components/PodDetail.jsx';
import Routes from './components/Routes.jsx';
import Docs from './components/Docs.jsx';
import styles from './../css/styles.css';
import { BrowserRouter, Redirect, Route, Switch } from 'react-router-dom';
import { LocaleProvider, Row, Col } from 'antd';
import enUS from 'antd/lib/locale-provider/en_US'; // configure ant locale globally

let appMain = document.getElementById('main');
let appData = appMain.dataset;

let pathPrefix = "";
let proxyPathMatch = window.location.pathname.match(/\/api\/v1\/namespaces\/.*\/proxy/g);
if (proxyPathMatch) {
  pathPrefix = proxyPathMatch[0];
}

ReactDOM.render((
  <LocaleProvider locale={enUS}>
    <BrowserRouter>
      <Row>
        <Col xs={6} sm={6}>
          <Route render={ routeProps => <Sidebar {...routeProps} goVersion={appData.goVersion} releaseVersion={appData.releaseVersion} pathPrefix={pathPrefix} uuid={appData.uuid} />} />
        </Col>
        <Col xs={18} sm={18}>
          <div className="main-content">
            <Switch>
              <Redirect exact from={`${pathPrefix}/`} to={`${pathPrefix}/servicemesh`} />
              <Route path={`${pathPrefix}/servicemesh`} render={() => <ServiceMesh pathPrefix={pathPrefix} releaseVersion={appData.releaseVersion} namespace={appData.namespace} />} />
              <Route path={`${pathPrefix}/deployments`} render={() => <Deployments pathPrefix={pathPrefix} namespace={appData.namespace}/>} />
              <Route path={`${pathPrefix}/deployment`} render={(props) => <Deployment pathPrefix={pathPrefix} location={props.location}/>} />
              <Route path={`${pathPrefix}/pod`} render={(props) => <PodDetail pathPrefix={pathPrefix} location={props.location}/>} />
              <Route path={`${pathPrefix}/routes`} render={() => <Routes pathPrefix={pathPrefix}/>} />
              <Route path={`${pathPrefix}/docs`} render={() => <Docs pathPrefix={pathPrefix}/>} />
              <Route component={NoMatch} />
            </Switch>
          </div>
        </Col>
      </Row>
    </BrowserRouter>
  </LocaleProvider>
), appMain);
