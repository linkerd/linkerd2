import _merge from 'lodash/merge';
import ApiHelpers from '../js/components/util/ApiHelpers.jsx';
import { createMemoryHistory } from 'history';
import React from 'react';
import { Route, Router } from 'react-router';

const componentDefaultProps = {
  api: ApiHelpers(''),
  controllerNamespace: 'shiny-controller-ns',
  productName: 'ShinyProductName',
  releaseVersion: ''
};

export function routerWrap(Component, extraProps={}, route="/", currentLoc="/") {
  const createElement = (ComponentToWrap, props) => (
    <ComponentToWrap {...(_merge({}, componentDefaultProps, props, extraProps))} />
  );
  return (
    <Router history={createMemoryHistory(currentLoc)} createElement={createElement}>
      <Route path={route} render={props => createElement(Component, props)} />
    </Router>
  );
}
