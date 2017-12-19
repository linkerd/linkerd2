import _ from 'lodash';
import { createMemoryHistory } from 'history';
import React from 'react';
import { Route, Router } from 'react-router';

export function routerWrap(Component, extraProps={}, route="/", currentLoc="/") {
  return (
    <Router history={createMemoryHistory(currentLoc)} createElement={(Component, props) => <Component {...(_.merge({}, props, extraProps))} />}>
      <Route path={route} component={Component} />
    </Router>
  );
}
