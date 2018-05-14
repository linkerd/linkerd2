import _ from 'lodash';
import { createMemoryHistory } from 'history';
import React from 'react';
import { Route, Router } from 'react-router';

export function routerWrap(Component, extraProps={}, route="/", currentLoc="/") {
  const createElement = (ComponentToWrap, props) => <ComponentToWrap {...(_.merge({}, props, extraProps))} />;
  return (
    <Router history={createMemoryHistory(currentLoc)} createElement={createElement}>
      <Route path={route} render={props => createElement(Component, props)} />
    </Router>
  );
}
