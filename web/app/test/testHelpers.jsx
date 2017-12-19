import React from 'react';
import { Route, Router } from 'react-router';
import { createMemoryHistory } from 'history';
import _ from 'lodash';

export function printStack() {
  let e = new Error('dummy');
  let stack = e.stack.replace(/^[^(]+?[\n$]/gm, '')
    .replace(/^\s+at\s+/gm, '')
    .replace(/^Object.<anonymous>\s*\(/gm, '{anonymous}()@')
    .split('\n');
  console.log(stack);
}

export function routerWrap(Component, extraProps={}, route="/", currentLoc="/") {
  return (
    <Router history={createMemoryHistory(currentLoc)} createElement={(Component, props) => <Component {...(_.merge({}, props, extraProps))} />}>
      <Route path={route} component={Component} />
    </Router>
  );
}